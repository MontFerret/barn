package barn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if err := os.Mkdir(filepath.Join(root, "modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "modules", ".gitkeep"), nil, 0o644); err != nil {
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
			oldPath := filepath.Join(root, "modules", "montferret", "archive", "versions", "v1.0.0.json")
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

func TestLoadRejectsEmptyModuleAndUnexpectedEntries(t *testing.T) {
	root := t.TempDir()
	writeRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"))
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "at least one version") {
		t.Fatalf("expected empty module failure, got %v", err)
	}

	writeJSON(t, filepath.Join(root, "modules", "montferret", "archive", "versions", "v1.0.0.json"), testVersion("1.0.0", "v1.0.0", testCommit))
	if err := os.WriteFile(filepath.Join(root, "modules", "montferret", "archive", "README.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "unexpected registry entry") {
		t.Fatalf("expected unexpected entry failure, got %v", err)
	}
}

func TestLoadRejectsMissingVersionFields(t *testing.T) {
	root := t.TempDir()
	writeRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))
	filePath := filepath.Join(root, "modules", "montferret", "archive", "versions", "v1.0.0.json")
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
	versions := filepath.Join(root, "modules", "montferret", "archive", "versions")
	if err := os.Symlink(filepath.Join(versions, "v1.0.0.json"), filepath.Join(versions, "v1.1.0.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("expected symlink to be rejected")
	}
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
