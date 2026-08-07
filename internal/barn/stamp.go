package barn

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	registryspec "github.com/MontFerret/specs/pkg/registry"
)

// ErrUnstampedVersions indicates that at least one canonical version record
// does not yet contain publication metadata.
var ErrUnstampedVersions = errors.New("registry contains unstamped versions")

// StampVersions assigns one UTC, whole-second publication timestamp to every
// canonical version record that does not already have one. Existing stamped
// records are never rewritten.
func StampVersions(root string, timestamp time.Time) (int, error) {
	if timestamp.IsZero() {
		return 0, fmt.Errorf("publication timestamp is zero")
	}

	registry, err := Load(root)
	if err != nil {
		return 0, err
	}

	publishedAt := timestamp.UTC().Truncate(time.Second)
	stamped := 0

	for _, module := range registry.Modules {
		for _, version := range module.Versions {
			if version.Record.PublishedAt != nil {
				continue
			}

			updated := *version.Record
			updated.PublishedAt = &publishedAt

			if err := registryspec.ValidateVersionRecord(&updated); err != nil {
				return stamped, fmt.Errorf("validate stamped version record %s: %w", version.Path, err)
			}

			if err := writeVersionRecord(version.Path, &updated); err != nil {
				return stamped, err
			}

			version.Record = &updated
			stamped++
		}
	}

	return stamped, nil
}

// CheckVersionsStamped reports whether every canonical version record has
// publication metadata without modifying the registry.
func CheckVersionsStamped(root string) error {
	registry, err := Load(root)
	if err != nil {
		return err
	}

	for _, module := range registry.Modules {
		for _, version := range module.Versions {
			if version.Record.PublishedAt == nil {
				relative, relativeErr := filepath.Rel(registry.Root, version.Path)
				if relativeErr != nil {
					relative = version.Path
				}

				return fmt.Errorf("%w: %s", ErrUnstampedVersions, filepath.ToSlash(relative))
			}
		}
	}

	return nil
}

func writeVersionRecord(filePath string, record *registryspec.VersionRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode stamped version record %s: %w", filePath, err)
	}

	data = append(data, '\n')
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("inspect version record %s: %w", filePath, err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(filePath), ".barn-stamp-*")
	if err != nil {
		return fmt.Errorf("create temporary version record for %s: %w", filePath, err)
	}

	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary version record permissions for %s: %w", filePath, err)
	}

	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary version record for %s: %w", filePath, err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary version record for %s: %w", filePath, err)
	}

	if err := os.Rename(temporaryPath, filePath); err != nil {
		return fmt.Errorf("replace version record %s: %w", filePath, err)
	}

	return nil
}
