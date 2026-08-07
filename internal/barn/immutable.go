package barn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	registryspec "github.com/MontFerret/specs/pkg/registry"
)

// CheckImmutable rejects changes to published release identities since base.
func CheckImmutable(ctx context.Context, root, base string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve Barn repository root: %w", err)
	}

	commit, err := runRepositoryGit(ctx, root, "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve immutability base %q: %w", base, err)
	}

	baseCommit := strings.TrimSpace(string(commit))
	listing, err := runRepositoryGit(ctx, root, "ls-tree", "-r", "--name-only", baseCommit, "--", moduleRegistryPath)
	if err != nil {
		return fmt.Errorf("list registry state at %s: %w", baseCommit, err)
	}

	paths := strings.Fields(string(listing))
	versionPaths := make([]string, 0)
	baseVersionPaths := make(map[string]struct{})
	manifestHasVersions := make(map[string]bool)

	for _, entryPath := range paths {
		parts := strings.Split(filepath.ToSlash(entryPath), "/")

		if len(parts) == 6 && strings.Join(parts[:2], "/") == moduleRegistryPath && parts[4] == "versions" && versionFilename.MatchString(parts[5]) {
			versionPaths = append(versionPaths, entryPath)
			baseVersionPaths[filepath.ToSlash(entryPath)] = struct{}{}
			manifestHasVersions[strings.Join(parts[:4], "/")+"/manifest.json"] = true
		}
	}

	for _, entryPath := range versionPaths {
		baseData, err := readGitFile(ctx, root, baseCommit, entryPath)
		if err != nil {
			return err
		}

		currentData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entryPath)))
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("published version record %s was deleted", entryPath)
			}

			return fmt.Errorf("read current version record %s: %w", entryPath, err)
		}

		baseRecord, basePublishedAt, err := parseImmutableVersionRecord(baseData)
		if err != nil {
			return fmt.Errorf("parse base version record %s: %w", entryPath, err)
		}

		currentRecord, currentPublishedAt, err := parseImmutableVersionRecord(currentData)
		if err != nil {
			return fmt.Errorf("parse current version record %s: %w", entryPath, err)
		}

		if !sameVersionRecordWithoutPublication(baseRecord, currentRecord) {
			return fmt.Errorf("published version record %s was modified", entryPath)
		}

		if basePublishedAt != nil && (currentPublishedAt == nil || *basePublishedAt != *currentPublishedAt) {
			return fmt.Errorf("published timestamp in version record %s was modified", entryPath)
		}
	}

	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(moduleRegistryPath))); statErr == nil {
		currentRegistry, err := Load(root)
		if err != nil {
			return err
		}

		for _, module := range currentRegistry.Modules {
			for _, version := range module.Versions {
				relative, err := filepath.Rel(root, version.Path)
				if err != nil {
					return fmt.Errorf("resolve version record path %s: %w", version.Path, err)
				}

				relative = filepath.ToSlash(relative)

				if _, existed := baseVersionPaths[relative]; !existed && version.Record.PublishedAt != nil {
					return fmt.Errorf("new version record %s must not contain publishedAt before publication", relative)
				}
			}
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect %s: %w", moduleRegistryPath, statErr)
	}

	for manifestPath := range manifestHasVersions {
		baseData, err := readGitFile(ctx, root, baseCommit, manifestPath)
		if err != nil {
			return err
		}

		currentData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifestPath)))
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("published module manifest %s was deleted", manifestPath)
			}

			return fmt.Errorf("read current module manifest %s: %w", manifestPath, err)
		}

		baseManifest, err := registryspec.ParseModuleManifest(baseData)
		if err != nil {
			return fmt.Errorf("parse base module manifest %s: %w", manifestPath, err)
		}

		currentManifest, err := registryspec.ParseModuleManifest(currentData)
		if err != nil {
			return fmt.Errorf("parse current module manifest %s: %w", manifestPath, err)
		}

		if baseManifest.Source != currentManifest.Source {
			return fmt.Errorf("source for published module %s in %s changed", baseManifest.Owner+"/"+baseManifest.Name, manifestPath)
		}
	}

	return nil
}

func parseImmutableVersionRecord(data []byte) (*registryspec.VersionRecord, *string, error) {
	record, err := registryspec.ParseVersionRecord(data)
	if err != nil {
		return nil, nil, err
	}

	var envelope struct {
		PublishedAt *string `json:"publishedAt"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, nil, err
	}

	return record, envelope.PublishedAt, nil
}

func sameVersionRecordWithoutPublication(left, right *registryspec.VersionRecord) bool {
	leftCopy := *left
	rightCopy := *right
	leftCopy.PublishedAt = nil
	rightCopy.PublishedAt = nil

	return reflect.DeepEqual(leftCopy, rightCopy)
}

func readGitFile(ctx context.Context, root, commit, entryPath string) ([]byte, error) {
	data, err := runRepositoryGit(ctx, root, "show", commit+":"+filepath.ToSlash(entryPath))
	if err != nil {
		return nil, fmt.Errorf("read %s at %s: %w", entryPath, commit, err)
	}

	return data, nil
}

func runRepositoryGit(ctx context.Context, root string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")

	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %s", strings.Join(arguments, " "), strings.TrimSpace(string(output)))
	}

	return output, nil
}
