package barn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Masterminds/semver/v3"
	registryspec "github.com/MontFerret/specs/pkg/registry"
)

const (
	moduleCatalogPath = "catalog/modules/index.json"
	pluginCatalogPath = "catalog/plugins/index.json"
)

type (
	// ModuleCatalog is the generated Registry Module Catalog v1 document.
	ModuleCatalog struct {
		SchemaVersion int                  `json:"schemaVersion"`
		Modules       []ModuleCatalogEntry `json:"modules"`
	}

	// ModuleCatalogEntry is the smallest installable and discoverable module projection.
	ModuleCatalogEntry struct {
		ID          string                 `json:"id"`
		Owner       string                 `json:"owner"`
		Name        string                 `json:"name"`
		Source      registryspec.Source    `json:"source"`
		Namespace   string                 `json:"namespace"`
		Description string                 `json:"description"`
		Latest      string                 `json:"latest,omitempty"`
		Versions    []ModuleCatalogVersion `json:"versions"`
	}

	// ModuleCatalogVersion is one immutable module catalog release.
	ModuleCatalogVersion struct {
		Version string `json:"version"`
		Ferret  string `json:"ferret,omitempty"`
		Commit  string `json:"commit"`
	}

	// PluginCatalog is the reserved generated Registry Plugin Catalog v1 document.
	// The plugin entry contract will be defined with plugin registry support.
	PluginCatalog struct {
		SchemaVersion int               `json:"schemaVersion"`
		Plugins       []json.RawMessage `json:"plugins"`
	}
)

// GenerateModuleCatalog produces deterministic module catalog JSON from a resolved Registry.
func GenerateModuleCatalog(registry *Registry) ([]byte, error) {
	modules := append([]*Module(nil), registry.Modules...)

	sort.Slice(modules, func(i, j int) bool { return modules[i].ID() < modules[j].ID() })

	catalog := ModuleCatalog{SchemaVersion: 1, Modules: make([]ModuleCatalogEntry, 0, len(modules))}

	for _, registryModule := range modules {
		versions := append([]*Version(nil), registryModule.Versions...)

		if err := sortVersions(versions); err != nil {
			return nil, fmt.Errorf("sort versions for %s: %w", registryModule.ID(), err)
		}

		metadataVersion := versions[0]
		latest := ""
		for _, version := range versions {
			parsed, _ := semver.StrictNewVersion(version.Record.Version)
			if parsed.Prerelease() == "" {
				latest = version.Record.Version
				metadataVersion = version

				break
			}
		}

		if metadataVersion.Manifest == nil {
			return nil, fmt.Errorf("module %s@%s has not been resolved", registryModule.ID(), metadataVersion.Record.Version)
		}

		catalogModule := ModuleCatalogEntry{
			ID:          registryModule.ID(),
			Owner:       registryModule.Manifest.Owner,
			Name:        registryModule.Manifest.Name,
			Source:      registryModule.Manifest.Source,
			Namespace:   metadataVersion.Manifest.Namespace,
			Description: metadataVersion.Manifest.Description,
			Latest:      latest,
			Versions:    make([]ModuleCatalogVersion, 0, len(versions)),
		}

		for _, version := range versions {
			if version.Manifest == nil {
				return nil, fmt.Errorf("module %s@%s has not been resolved", registryModule.ID(), version.Record.Version)
			}

			ferret := ""
			if version.Manifest.Compatibility != nil {
				ferret = version.Manifest.Compatibility.Ferret
			}

			catalogModule.Versions = append(catalogModule.Versions, ModuleCatalogVersion{
				Version: version.Record.Version,
				Ferret:  ferret,
				Commit:  version.Record.Commit,
			})
		}

		catalog.Modules = append(catalog.Modules, catalogModule)
	}

	return encodeCatalog(catalog, "module")
}

// GeneratePluginCatalog produces the deterministic empty Plugin Catalog v1 document.
func GeneratePluginCatalog() ([]byte, error) {
	catalog := PluginCatalog{SchemaVersion: 1, Plugins: make([]json.RawMessage, 0)}

	return encodeCatalog(catalog, "plugin")
}

func encodeCatalog(catalog any, kind string) ([]byte, error) {
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode %s catalog: %w", kind, err)
	}

	return append(data, '\n'), nil
}

func sortVersions(versions []*Version) error {
	parsed := make(map[*Version]*semver.Version, len(versions))

	for _, version := range versions {
		value, err := semver.StrictNewVersion(version.Record.Version)
		if err != nil {
			return err
		}

		parsed[version] = value
	}

	sort.Slice(versions, func(i, j int) bool {
		comparison := parsed[versions[i]].Compare(parsed[versions[j]])
		if comparison != 0 {
			return comparison > 0
		}

		return versions[i].Record.Version < versions[j].Record.Version
	})

	return nil
}

// WriteModuleCatalog atomically replaces the checked-in module catalog.
func WriteModuleCatalog(root string, data []byte) error {
	return writeCatalog(root, "modules", data)
}

// WritePluginCatalog atomically replaces the checked-in plugin catalog.
func WritePluginCatalog(root string, data []byte) error {
	return writeCatalog(root, "plugins", data)
}

func writeCatalog(root, kind string, data []byte) error {
	relativePath := filepath.Join("catalog", kind, "index.json")
	directory := filepath.Join(root, "catalog", kind)

	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create %s catalog directory: %w", kind, err)
	}

	temporary, err := os.CreateTemp(directory, ".index-*.json")
	if err != nil {
		return fmt.Errorf("create temporary catalog: %w", err)
	}

	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()

		return fmt.Errorf("set temporary catalog permissions: %w", err)
	}

	if _, err := temporary.Write(data); err != nil {
		temporary.Close()

		return fmt.Errorf("write temporary catalog: %w", err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary catalog: %w", err)
	}

	if err := os.Rename(temporaryPath, filepath.Join(directory, "index.json")); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.ToSlash(relativePath), err)
	}

	return nil
}

// VerifyModuleCatalog fails when the checked-in module catalog differs from generated data.
func VerifyModuleCatalog(root string, generated []byte) error {
	return verifyCatalog(root, moduleCatalogPath, generated)
}

// VerifyPluginCatalog fails when the checked-in plugin catalog differs from generated data.
func VerifyPluginCatalog(root string, generated []byte) error {
	return verifyCatalog(root, pluginCatalogPath, generated)
}

func verifyCatalog(root, relativePath string, generated []byte) error {
	current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		return fmt.Errorf("read checked-in %s: %w", relativePath, err)
	}

	if !bytes.Equal(current, generated) {
		return fmt.Errorf("%s is stale; run barn generate", relativePath)
	}

	return nil
}
