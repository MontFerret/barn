package barn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	modulemanifest "github.com/MontFerret/specs/pkg/module"
)

func TestGenerateModuleCatalogDeterministicOrderingAndLatest(t *testing.T) {
	archive := catalogTestModule("montferret", "archive", []catalogTestVersion{
		{version: "1.1.0-beta.1", namespace: "ARCHIVE_BETA", description: "Beta.", ferret: ">=2.2.0-beta.1"},
		{version: "1.0.0", namespace: "ARCHIVE", description: "Stable.", ferret: ">=2.1.0"},
		{version: "1.0.0+build.2", namespace: "ARCHIVE_BUILD", description: "Build.", ferret: ""},
	})
	browser := catalogTestModule("acme", "browser-tools", []catalogTestVersion{{
		version: "2.0.0-beta.2", namespace: "BROWSER", description: "Browser beta.", ferret: "^2.0.0",
	}})
	registry := &Registry{Modules: []*Module{archive, browser}}
	first, err := GenerateModuleCatalog(registry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateModuleCatalog(registry)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("catalog generation is not deterministic")
	}
	var catalog ModuleCatalog
	if err := json.Unmarshal(first, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Modules) != 2 || catalog.Modules[0].ID != "acme/browser-tools" || catalog.Modules[1].ID != "montferret/archive" {
		t.Fatalf("modules not sorted: %#v", catalog.Modules)
	}
	if catalog.Modules[0].Latest != "" || catalog.Modules[0].Namespace != "BROWSER" {
		t.Fatalf("prerelease-only behavior differs: %#v", catalog.Modules[0])
	}
	archiveCatalog := catalog.Modules[1]
	if archiveCatalog.Latest != "1.0.0" || archiveCatalog.Description != "Stable." {
		t.Fatalf("stable latest differs: %#v", archiveCatalog)
	}
	wantVersions := []string{"1.1.0-beta.1", "1.0.0", "1.0.0+build.2"}
	for index, want := range wantVersions {
		if archiveCatalog.Versions[index].Version != want {
			t.Fatalf("version order differs: %#v", archiveCatalog.Versions)
		}
	}
}

func TestGenerateEmptyPluginCatalog(t *testing.T) {
	generated, err := GeneratePluginCatalog()
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"schemaVersion\": 1,\n  \"plugins\": []\n}\n"
	if string(generated) != want {
		t.Fatalf("plugin catalog differs:\n%s", generated)
	}
}

func TestCatalogWriteAndVerification(t *testing.T) {
	root := t.TempDir()
	moduleCatalog, err := GenerateModuleCatalog(&Registry{})
	if err != nil {
		t.Fatal(err)
	}
	pluginCatalog, err := GeneratePluginCatalog()
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifyModuleCatalog(root, moduleCatalog); err == nil || !strings.Contains(err.Error(), moduleCatalogPath) {
		t.Fatalf("expected missing module catalog error, got %v", err)
	}
	if err := VerifyPluginCatalog(root, pluginCatalog); err == nil || !strings.Contains(err.Error(), pluginCatalogPath) {
		t.Fatalf("expected missing plugin catalog error, got %v", err)
	}
	if err := WriteModuleCatalog(root, moduleCatalog); err != nil {
		t.Fatal(err)
	}
	if err := WritePluginCatalog(root, pluginCatalog); err != nil {
		t.Fatal(err)
	}
	if err := VerifyModuleCatalog(root, moduleCatalog); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPluginCatalog(root, pluginCatalog); err != nil {
		t.Fatal(err)
	}

	modulePath := filepath.Join(root, filepath.FromSlash(moduleCatalogPath))
	if err := os.WriteFile(modulePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyModuleCatalog(root, moduleCatalog); err == nil || !strings.Contains(err.Error(), moduleCatalogPath) {
		t.Fatalf("expected stale module catalog error, got %v", err)
	}
	if err := WriteModuleCatalog(root, moduleCatalog); err != nil {
		t.Fatal(err)
	}

	pluginPath := filepath.Join(root, filepath.FromSlash(pluginCatalogPath))
	if err := os.WriteFile(pluginPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPluginCatalog(root, pluginCatalog); err == nil || !strings.Contains(err.Error(), pluginCatalogPath) {
		t.Fatalf("expected stale plugin catalog error, got %v", err)
	}
}

type catalogTestVersion struct {
	version     string
	namespace   string
	description string
	ferret      string
}

func catalogTestModule(owner, name string, fixtures []catalogTestVersion) *Module {
	versions := make([]*Version, 0, len(fixtures))
	for index, fixture := range fixtures {
		manifest := testModuleManifest(owner+"/"+name, fixture.namespace, fixture.version, fixture.description)
		if fixture.ferret != "" {
			manifest.Compatibility = &modulemanifest.Compatibility{Ferret: fixture.ferret}
		}
		versions = append(versions, &Version{
			Record:   testVersion(fixture.version, "v"+fixture.version, testCommit[:39]+string(rune('0'+index))),
			Manifest: manifest,
		})
	}
	return &Module{Manifest: testRegistryManifest(owner, name), Versions: versions}
}
