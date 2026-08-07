package barn

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	registryspec "github.com/MontFerret/specs/pkg/registry"
)

var versionFilename = regexp.MustCompile(`^v(.+)\.json$`)

// Load reads and validates the repository-local registry source layout.
func Load(root string) (*Registry, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve registry root: %w", err)
	}

	modulesRoot := filepath.Join(root, "modules")
	ownerEntries, err := readDirectory(modulesRoot)
	if err != nil {
		return nil, fmt.Errorf("read modules directory: %w", err)
	}

	result := &Registry{Root: root, Modules: make([]*Module, 0)}
	identities := make(map[string]string)

	for _, ownerEntry := range ownerEntries {
		if ownerEntry.Name() == ".gitkeep" && !ownerEntry.IsDir() {
			continue
		}

		if err := requireDirectory(ownerEntry, filepath.Join("modules", ownerEntry.Name())); err != nil {
			return nil, err
		}

		owner := ownerEntry.Name()
		ownerPath := filepath.Join(modulesRoot, owner)
		moduleEntries, err := readDirectory(ownerPath)
		if err != nil {
			return nil, fmt.Errorf("read owner directory %q: %w", owner, err)
		}

		for _, moduleEntry := range moduleEntries {
			relative := filepath.Join("modules", owner, moduleEntry.Name())
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

func addIdentity(identities map[string]string, module *Module) error {
	if previous, exists := identities[module.ID()]; exists {
		return fmt.Errorf("duplicate module identity %q in %s and %s", module.ID(), previous, module.Directory)
	}

	identities[module.ID()] = module.Directory

	return nil
}

func loadModule(root, owner, name string) (*Module, error) {
	directory := filepath.Join(root, "modules", owner, name)
	entries, err := readDirectory(directory)
	if err != nil {
		return nil, fmt.Errorf("read module directory %q: %w", filepath.Join(owner, name), err)
	}

	seenManifest := false
	seenVersions := false

	for _, entry := range entries {
		relative := filepath.Join("modules", owner, name, entry.Name())
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
		return nil, fmt.Errorf("module %s/%s must contain manifest.json and versions/", owner, name)
	}

	manifestPath := filepath.Join(directory, "manifest.json")
	manifest, err := registryspec.LoadModuleManifestFile(manifestPath)
	if err != nil {
		return nil, err
	}

	if manifest.Owner != owner {
		return nil, fmt.Errorf("module manifest owner %q does not match directory owner %q", manifest.Owner, owner)
	}

	if manifest.Name != name {
		return nil, fmt.Errorf("module manifest name %q does not match directory name %q", manifest.Name, name)
	}

	versions, err := loadVersions(root, owner, name)
	if err != nil {
		return nil, err
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("module %s/%s must contain at least one version record", owner, name)
	}

	return &Module{Directory: directory, Manifest: manifest, Versions: versions}, nil
}

func loadVersions(root, owner, name string) ([]*Version, error) {
	directory := filepath.Join(root, "modules", owner, name, "versions")
	entries, err := readDirectory(directory)
	if err != nil {
		return nil, fmt.Errorf("read versions for %s/%s: %w", owner, name, err)
	}

	versions := make([]*Version, 0, len(entries))

	for _, entry := range entries {
		relative := filepath.Join("modules", owner, name, "versions", entry.Name())

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
			return nil, fmt.Errorf("version filename %q does not match declared version %q", entry.Name(), record.Version)
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
