package barn

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	registryspec "github.com/MontFerret/specs/pkg/registry"
)

const (
	registrySourcePath = "registry"
	moduleRegistryPath = registrySourcePath + "/modules"
	pluginRegistryPath = registrySourcePath + "/plugins"
)

var versionFilename = regexp.MustCompile(`^v(.+)\.json$`)

// Load reads and validates the repository-local registry source layout.
func Load(root string) (*Registry, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve Barn repository root: %w", err)
	}

	modulesRoot := filepath.Join(root, filepath.FromSlash(moduleRegistryPath))
	ownerEntries, err := readDirectory(modulesRoot)
	if err != nil {
		return nil, fmt.Errorf("read %s directory: %w", moduleRegistryPath, err)
	}

	if err := validateReservedPluginRoot(root); err != nil {
		return nil, err
	}

	result := &Registry{Root: root, Modules: make([]*Module, 0)}
	identities := make(map[string]string)

	for _, ownerEntry := range ownerEntries {
		if ownerEntry.Name() == ".gitkeep" {
			if ownerEntry.IsDir() || ownerEntry.Type()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("%s must be a regular file", path.Join(moduleRegistryPath, ownerEntry.Name()))
			}

			continue
		}

		if err := requireDirectory(ownerEntry, path.Join(moduleRegistryPath, ownerEntry.Name())); err != nil {
			return nil, err
		}

		owner := ownerEntry.Name()
		ownerPath := filepath.Join(modulesRoot, owner)
		moduleEntries, err := readDirectory(ownerPath)
		if err != nil {
			return nil, fmt.Errorf("read owner directory %q: %w", owner, err)
		}

		for _, moduleEntry := range moduleEntries {
			relative := path.Join(moduleRegistryPath, owner, moduleEntry.Name())
			if err := requireDirectory(moduleEntry, relative); err != nil {
				return nil, err
			}

			loaded, err := loadModule(root, owner, moduleEntry.Name())
			if err != nil {
				return nil, err
			}

			if err := addIdentity(identities, loaded); err != nil {
				return nil, err
			}

			result.Modules = append(result.Modules, loaded)
		}
	}

	return result, nil
}

func validateReservedPluginRoot(root string) error {
	entries, err := readDirectory(filepath.Join(root, filepath.FromSlash(pluginRegistryPath)))
	if err != nil {
		return fmt.Errorf("read %s directory: %w", pluginRegistryPath, err)
	}

	for _, entry := range entries {
		relative := path.Join(pluginRegistryPath, entry.Name())
		if entry.Name() == ".gitkeep" && !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			continue
		}

		return fmt.Errorf("unexpected registry entry %s: plugin registrations are not supported", relative)
	}

	return nil
}

func addIdentity(identities map[string]string, module *Module) error {
	if previous, exists := identities[module.ID()]; exists {
		return fmt.Errorf("duplicate module identity %q in %s and %s", module.ID(), previous, module.Directory)
	}

	identities[module.ID()] = module.Directory

	return nil
}

func loadModule(root, owner, name string) (*Module, error) {
	relativeDirectory := path.Join(moduleRegistryPath, owner, name)
	directory := filepath.Join(root, filepath.FromSlash(relativeDirectory))
	entries, err := readDirectory(directory)
	if err != nil {
		return nil, fmt.Errorf("read module directory %q: %w", relativeDirectory, err)
	}

	seenManifest := false
	seenVersions := false

	for _, entry := range entries {
		relative := path.Join(relativeDirectory, entry.Name())
		switch entry.Name() {
		case "manifest.json":
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("%s must be a regular file", relative)
			}

			seenManifest = true
		case "versions":
			if err := requireDirectory(entry, relative); err != nil {
				return nil, err
			}

			seenVersions = true
		default:
			return nil, fmt.Errorf("unexpected registry entry %s", relative)
		}
	}

	if !seenManifest || !seenVersions {
		return nil, fmt.Errorf("module %s must contain manifest.json and versions/", relativeDirectory)
	}

	manifestPath := filepath.Join(directory, "manifest.json")
	manifest, err := registryspec.LoadModuleManifestFile(manifestPath)
	if err != nil {
		return nil, err
	}

	if manifest.Owner != owner {
		return nil, fmt.Errorf("module manifest %s owner %q does not match directory owner %q", path.Join(relativeDirectory, "manifest.json"), manifest.Owner, owner)
	}

	if manifest.Name != name {
		return nil, fmt.Errorf("module manifest %s name %q does not match directory name %q", path.Join(relativeDirectory, "manifest.json"), manifest.Name, name)
	}

	versions, err := loadVersions(root, owner, name)
	if err != nil {
		return nil, err
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("module %s must contain at least one version record", relativeDirectory)
	}

	return &Module{Directory: directory, Manifest: manifest, Versions: versions}, nil
}

func loadVersions(root, owner, name string) ([]*Version, error) {
	relativeDirectory := path.Join(moduleRegistryPath, owner, name, "versions")
	directory := filepath.Join(root, filepath.FromSlash(relativeDirectory))
	entries, err := readDirectory(directory)
	if err != nil {
		return nil, fmt.Errorf("read versions directory %s: %w", relativeDirectory, err)
	}

	versions := make([]*Version, 0, len(entries))

	for _, entry := range entries {
		relative := path.Join(relativeDirectory, entry.Name())

		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("version record %s must be a regular file", relative)
		}

		match := versionFilename.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("unexpected registry entry %s", relative)
		}

		filePath := filepath.Join(directory, entry.Name())
		record, err := registryspec.LoadVersionRecordFile(filePath)
		if err != nil {
			return nil, err
		}

		if match[1] != record.Version {
			return nil, fmt.Errorf("version record %s declares version %q, which does not match its filename", relative, record.Version)
		}

		versions = append(versions, &Version{Path: filePath, Record: record})
	}

	return versions, nil
}

func readDirectory(path string) ([]os.DirEntry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a real directory", path)
	}

	return os.ReadDir(path)
}

func requireDirectory(entry os.DirEntry, relative string) error {
	if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
		return fmt.Errorf("%s must be a real directory", strings.TrimPrefix(filepath.ToSlash(relative), "./"))
	}

	return nil
}
