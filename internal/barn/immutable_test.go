package barn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckImmutableAllowsAdditiveVersion(t *testing.T) {
	root, base := committedRegistryFixture(t)
	writeJSON(t, filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "montferret", "archive", "versions", "v1.1.0.json"), testVersion("1.1.0", "v1.1.0", strings.Repeat("a", 40)))
	if err := CheckImmutable(context.Background(), root, base); err != nil {
		t.Fatal(err)
	}
}

func TestCheckImmutableRejectsPublishedMutations(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, string){
		"version changed": func(t *testing.T, root string) {
			writeJSON(t, filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "montferret", "archive", "versions", "v1.0.0.json"), testVersion("1.0.0", "retagged/v1.0.0", testCommit))
		},
		"version deleted": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "montferret", "archive", "versions", "v1.0.0.json")); err != nil {
				t.Fatal(err)
			}
		},
		"manifest deleted": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "montferret", "archive", "manifest.json")); err != nil {
				t.Fatal(err)
			}
		},
		"repository changed": func(t *testing.T, root string) {
			manifest := testRegistryManifest("montferret", "archive")
			manifest.Source.Repository = "https://example.org/other.git"
			writeJSON(t, filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "montferret", "archive", "manifest.json"), manifest)
		},
		"source path changed": func(t *testing.T, root string) {
			manifest := testRegistryManifest("montferret", "archive")
			manifest.Source.Path = "modules/archive"
			writeJSON(t, filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "montferret", "archive", "manifest.json"), manifest)
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, base := committedRegistryFixture(t)
			mutate(t, root)
			if err := CheckImmutable(context.Background(), root, base); err == nil || !strings.Contains(err.Error(), moduleRegistryPath+"/montferret/archive") {
				t.Fatalf("expected mutation at canonical path to be rejected, got %v", err)
			}
		})
	}
}

func TestCheckImmutableDoesNotScanLegacyModuleRoot(t *testing.T) {
	root := t.TempDir()
	runTestGit(t, root, "init")
	runTestGit(t, root, "config", "user.name", "Barn Tests")
	runTestGit(t, root, "config", "user.email", "barn-tests@example.org")
	writeLegacyRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))
	runTestGit(t, root, "add", ".")
	runTestGit(t, root, "commit", "-m", "legacy registry state")
	base := strings.TrimSpace(runTestGit(t, root, "rev-parse", "HEAD"))
	writeJSON(t, filepath.Join(root, "modules", "montferret", "archive", "versions", "v1.0.0.json"), testVersion("1.0.0", "retagged/v1.0.0", testCommit))

	if err := CheckImmutable(context.Background(), root, base); err != nil {
		t.Fatalf("legacy module root was scanned: %v", err)
	}
}

func committedRegistryFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	runTestGit(t, root, "init")
	runTestGit(t, root, "config", "user.name", "Barn Tests")
	runTestGit(t, root, "config", "user.email", "barn-tests@example.org")
	writeRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))
	runTestGit(t, root, "add", ".")
	runTestGit(t, root, "commit", "-m", "published registry state")
	return root, strings.TrimSpace(runTestGit(t, root, "rev-parse", "HEAD"))
}
