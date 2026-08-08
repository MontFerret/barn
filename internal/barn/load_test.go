package barn

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MontFerret/specs/pkg/validation"
)

func TestLoadValidRegistryLayout(t *testing.T) {
	root := t.TempDir()
	writeRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))
	registry, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Modules) != 1 || registry.Modules[0].ID() != "montferret/archive" || len(registry.Modules[0].Versions) != 1 {
		t.Fatalf("unexpected registry: %#v", registry)
	}
}

func TestLoadEmptyRegistry(t *testing.T) {
	root := t.TempDir()
	ensureRegistryRoots(t, root)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(moduleRegistryPath), ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(pluginRegistryPath), ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Modules) != 0 {
		t.Fatalf("expected no modules: %#v", registry.Modules)
	}
}

func TestLoadRejectsIdentityAndFilenameMismatches(t *testing.T) {
	for name, setup := range map[string]func(*testing.T, string){
		"owner": func(t *testing.T, root string) {
			writeRegistryRecord(t, root, "acme", "archive", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))
		},
		"module": func(t *testing.T, root string) {
			writeRegistryRecord(t, root, "montferret", "other", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))
		},
		"version filename": func(t *testing.T, root string) {
			writeRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))
			oldPath := filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "montferret", "archive", "versions", "v1.0.0.json")
			if err := os.Rename(oldPath, filepath.Join(filepath.Dir(oldPath), "v2.0.0.json")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			setup(t, root)
			if _, err := Load(root); err == nil {
				t.Fatal("expected layout to be rejected")
			}
		})
	}
}

func TestLoadRejectsNonCanonicalRegistryIdentity(t *testing.T) {
	for _, test := range []struct {
		name   string
		owner  string
		module string
		path   string
	}{
		{name: "uppercase owner", owner: "MONTFERRET", module: "archive", path: "/owner"},
		{name: "mixed-case owner", owner: "MontFerret", module: "archive", path: "/owner"},
		{name: "uppercase module", owner: "montferret", module: "ARCHIVE", path: "/name"},
		{name: "mixed-case module", owner: "montferret", module: "Archive", path: "/name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeRegistryRecord(
				t,
				root,
				test.owner,
				test.module,
				testRegistryManifest(test.owner, test.module),
				testVersion("1.0.0", "v1.0.0", testCommit),
			)

			_, err := Load(root)
			var validationErr *validation.Errors
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected Specs validation error, got %T: %v", err, err)
			}
			requireRegistryViolation(t, validationErr, test.path, validation.Rule("pattern"))
		})
	}
}

func TestLoadRejectsEmptyModuleAndUnexpectedEntries(t *testing.T) {
	root := t.TempDir()
	writeRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"))
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "at least one version") {
		t.Fatalf("expected empty module failure, got %v", err)
	}

	moduleDirectory := filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "montferret", "archive")
	writeJSON(t, filepath.Join(moduleDirectory, "versions", "v1.0.0.json"), testVersion("1.0.0", "v1.0.0", testCommit))
	if err := os.WriteFile(filepath.Join(moduleDirectory, "README.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), moduleRegistryPath+"/montferret/archive/README.md") {
		t.Fatalf("expected unexpected entry failure, got %v", err)
	}
}

func TestLoadRejectsMissingVersionFields(t *testing.T) {
	root := t.TempDir()
	writeRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))
	filePath := filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "montferret", "archive", "versions", "v1.0.0.json")
	if err := os.WriteFile(filePath, []byte(`{"$schema":"https://schemas.ferretlang.org/registry/version/v1.json","version":"1.0.0","tag":"v1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("expected missing commit to be rejected")
	}
}

func TestLoadRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	writeRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))
	versions := filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "montferret", "archive", "versions")
	if err := os.Symlink(filepath.Join(versions, "v1.0.0.json"), filepath.Join(versions, "v1.1.0.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), moduleRegistryPath+"/montferret/archive/versions/v1.1.0.json") {
		t.Fatalf("expected symlink path to be rejected, got %v", err)
	}
}

func TestLoadDoesNotUseLegacyRegistrationRoots(t *testing.T) {
	t.Run("ignored when canonical roots exist", func(t *testing.T) {
		root := t.TempDir()
		ensureRegistryRoots(t, root)
		writeLegacyRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))
		if err := os.MkdirAll(filepath.Join(root, "plugins", "montferret", "archive"), 0o755); err != nil {
			t.Fatal(err)
		}

		registry, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(registry.Modules) != 0 {
			t.Fatalf("legacy module root was discovered: %#v", registry.Modules)
		}
	})

	t.Run("cannot replace canonical module root", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(pluginRegistryPath)), 0o755); err != nil {
			t.Fatal(err)
		}
		writeLegacyRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))

		if _, err := Load(root); err == nil || !strings.Contains(err.Error(), moduleRegistryPath) {
			t.Fatalf("expected missing canonical module root failure, got %v", err)
		}
	})
}

func TestLoadValidatesReservedPluginRoot(t *testing.T) {
	t.Run("required", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(moduleRegistryPath)), 0o755); err != nil {
			t.Fatal(err)
		}

		if _, err := Load(root); err == nil || !strings.Contains(err.Error(), pluginRegistryPath) {
			t.Fatalf("expected missing plugin root failure, got %v", err)
		}
	})

	t.Run("registration entries rejected", func(t *testing.T) {
		root := t.TempDir()
		ensureRegistryRoots(t, root)
		unexpected := filepath.Join(root, filepath.FromSlash(pluginRegistryPath), "montferret")
		if err := os.Mkdir(unexpected, 0o755); err != nil {
			t.Fatal(err)
		}

		if _, err := Load(root); err == nil || !strings.Contains(err.Error(), pluginRegistryPath+"/montferret") {
			t.Fatalf("expected reserved plugin entry failure, got %v", err)
		}
	})
}

func TestDuplicateIdentityDetection(t *testing.T) {
	identities := make(map[string]string)
	first := &Module{Directory: "first", Manifest: testRegistryManifest("montferret", "archive")}
	second := &Module{Directory: "second", Manifest: testRegistryManifest("montferret", "archive")}
	if err := addIdentity(identities, first); err != nil {
		t.Fatal(err)
	}
	if err := addIdentity(identities, second); err == nil {
		t.Fatal("expected duplicate identity to be rejected")
	}
}

func requireRegistryViolation(t *testing.T, validationErr *validation.Errors, path string, rule validation.Rule) {
	t.Helper()
	for _, violation := range validationErr.Violations {
		if violation.Path == path && violation.Rule == rule {
			return
		}
	}

	t.Fatalf("missing violation path=%q rule=%q in %#v", path, rule, validationErr.Violations)
}
