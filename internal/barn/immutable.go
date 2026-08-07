package barn

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	registryspec "github.com/MontFerret/specs/pkg/registry"
)

// CheckImmutable rejects changes to published release identities since base.
func CheckImmutable(ctx context.Context, root, base string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve registry root: %w", err)
	}

	commit, err := runRepositoryGit(ctx, root, "rev-parse", "--verify", "--end-of-options", base+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve immutability base %q: %w", base, err)
	}

	baseCommit := strings.TrimSpace(string(commit))
	listing, err := runRepositoryGit(ctx, root, "ls-tree", "-r", "--name-only", baseCommit, "--", "modules")
	if err != nil {
		return fmt.Errorf("list registry state at %s: %w", baseCommit, err)
	}

	paths := strings.Fields(string(listing))
	versionPaths := make([]string, 0)
	manifestHasVersions := make(map[string]bool)

	for _, entryPath := range paths {
		parts := strings.Split(filepath.ToSlash(entryPath), "/")

		if len(parts) == 5 && parts[0] == "modules" && parts[3] == "versions" && versionFilename.MatchString(parts[4]) {
			versionPaths = append(versionPaths, entryPath)
			manifestHasVersions[strings.Join(parts[:3], "/")+"/manifest.json"] = true
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

		baseRecord, err := registryspec.ParseVersionRecord(baseData)
		if err != nil {
			return fmt.Errorf("parse base version record %s: %w", entryPath, err)
		}

		currentRecord, err := registryspec.ParseVersionRecord(currentData)
		if err != nil {
			return fmt.Errorf("parse current version record %s: %w", entryPath, err)
		}

		if *baseRecord != *currentRecord {
			return fmt.Errorf("published version record %s was modified", entryPath)
		}
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
			return fmt.Errorf("source for published module %s changed", baseManifest.Owner+"/"+baseManifest.Name)
		}
	}

	return nil
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
