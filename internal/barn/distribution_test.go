package barn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	modulemanifest "github.com/MontFerret/specs/pkg/module"
	registryartifact "github.com/MontFerret/specs/pkg/registry/artifact"
)

func TestCanonicalRegistryLayoutGeneratesDistribution(t *testing.T) {
	const (
		sourcePath    = "modules/archive"
		documentation = "# Archive\n\nPinned documentation.\n"
	)

	sourceManifest := testModuleManifest("montferret/archive", "ARCHIVE", "1.2.0", "Work with archives.")
	sourceManifest.Repository.Directory = sourcePath
	sourceManifest.Categories = []string{"files"}
	sourceManifest.Links = map[string]string{"homepage": "https://example.org/archive"}
	fixture := newGitFixtureWithDocumentation(t, sourcePath, sourceManifest, "archive/v1.2.0", true, []byte(documentation))
	registryManifest := testRegistryManifest("montferret", "archive")
	registryManifest.Source.Path = sourcePath
	root := t.TempDir()
	writeRegistryRecord(t, root, "montferret", "archive", registryManifest, stampedTestVersion("1.2.0", "archive/v1.2.0", fixture.commit))

	registry, err := Validate(context.Background(), root, fixtureGitInspector(fixture.directory))
	if err != nil {
		t.Fatal(err)
	}
	distribution, err := GenerateDistribution(registry)
	if err != nil {
		t.Fatal(err)
	}

	wantPaths := []string{
		"categories.json",
		"categories/files.json",
		"index.json",
		"modules/index.json",
		"modules/montferret/archive/index.json",
		"modules/montferret/archive/versions/1.2.0/api.json",
		"modules/montferret/archive/versions/1.2.0/docs.html",
		"modules/montferret/archive/versions/1.2.0/docs.md",
		"modules/montferret/archive/versions/1.2.0/index.json",
		"plugins/index.json",
	}
	if got := sortedDistributionPaths(distribution.Files); !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("distribution paths differ:\ngot  %v\nwant %v", got, wantPaths)
	}
	assertCanonicalArtifactDocuments(t, distribution, map[string]func([]byte) error{
		"index.json": func(data []byte) error {
			_, err := registryartifact.ParseRootIndex(data)
			return err
		},
		"modules/index.json": func(data []byte) error {
			_, err := registryartifact.ParseModuleIndex(data)
			return err
		},
		"modules/montferret/archive/index.json": func(data []byte) error {
			_, err := registryartifact.ParseModuleDocument(data)
			return err
		},
		"modules/montferret/archive/versions/1.2.0/index.json": func(data []byte) error {
			_, err := registryartifact.ParseVersionDocument(data)
			return err
		},
		"modules/montferret/archive/versions/1.2.0/api.json": func(data []byte) error {
			_, err := registryartifact.ParseAPIReference(data)
			return err
		},
		"categories.json": func(data []byte) error {
			_, err := registryartifact.ParseCategoryIndex(data)
			return err
		},
		"categories/files.json": func(data []byte) error {
			_, err := registryartifact.ParseCategoryDocument(data)
			return err
		},
		"plugins/index.json": func(data []byte) error {
			_, err := registryartifact.ParsePluginIndex(data)
			return err
		},
	})

	var rootIndex RootIndex
	decodeDistributionJSON(t, distribution, "index.json", &rootIndex)
	if rootIndex.SchemaVersion != 1 || !reflect.DeepEqual(rootIndex.Artifacts, map[string]string{
		"categories": "/categories.json",
		"modules":    "/modules/index.json",
		"plugins":    "/plugins/index.json",
	}) {
		t.Fatalf("unexpected root index: %#v", rootIndex)
	}

	var moduleIndex ModuleIndex
	decodeDistributionJSON(t, distribution, "modules/index.json", &moduleIndex)
	wantIndexEntry := ModuleIndexEntry{
		ID:     "montferret/archive",
		Latest: "1.2.0",
		Href:   "/modules/montferret/archive/index.json",
	}
	if len(moduleIndex.Modules) != 1 || moduleIndex.Modules[0] != wantIndexEntry {
		t.Fatalf("unexpected module index: %#v", moduleIndex)
	}
	if data := string(distribution.Files["modules/index.json"]); strings.Contains(data, "description") || strings.Contains(data, "namespace") || strings.Contains(data, "source") || strings.Contains(data, "versions") {
		t.Fatalf("module collection embeds detail metadata:\n%s", data)
	}

	var categoryIndex CategoryIndex
	decodeDistributionJSON(t, distribution, "categories.json", &categoryIndex)
	if !reflect.DeepEqual(categoryIndex.Categories, []CategoryIndexEntry{{
		ID:    "files",
		Name:  "Files",
		Count: 1,
		Href:  "/categories/files.json",
	}}) {
		t.Fatalf("unexpected category index: %#v", categoryIndex)
	}

	var categoryDocument CategoryDocument
	decodeDistributionJSON(t, distribution, "categories/files.json", &categoryDocument)
	if categoryDocument.Category != (CategorySummary{ID: "files", Name: "Files"}) || !reflect.DeepEqual(categoryDocument.Modules, []ModuleIndexEntry{wantIndexEntry}) {
		t.Fatalf("unexpected category document: %#v", categoryDocument)
	}

	var moduleDocument ModuleDocument
	decodeDistributionJSON(t, distribution, "modules/montferret/archive/index.json", &moduleDocument)
	if moduleDocument.ID != "montferret/archive" || moduleDocument.Owner != "montferret" || moduleDocument.Name != "archive" || moduleDocument.Description != "Work with archives." || moduleDocument.Latest != "1.2.0" {
		t.Fatalf("unexpected module document: %#v", moduleDocument)
	}
	if !reflect.DeepEqual(moduleDocument.Versions, []ModuleDocumentVersion{{
		Version:     "1.2.0",
		PublishedAt: testPublishedAt,
		Href:        "/modules/montferret/archive/versions/1.2.0/index.json",
	}}) {
		t.Fatalf("unexpected module versions: %#v", moduleDocument.Versions)
	}

	versionPath := "modules/montferret/archive/versions/1.2.0/index.json"
	var versionDocument VersionDocument
	decodeDistributionJSON(t, distribution, versionPath, &versionDocument)
	if versionDocument.ID != "montferret/archive" || versionDocument.Version != "1.2.0" || versionDocument.Description != "Work with archives." || versionDocument.Namespace != "ARCHIVE" || versionDocument.License != "Apache-2.0" {
		t.Fatalf("unexpected version identity: %#v", versionDocument)
	}
	wantSource := VersionSource{
		Repository: "https://fixtures.invalid/source.git",
		Path:       sourcePath,
		Commit:     fixture.commit,
	}
	if versionDocument.Source != wantSource || versionDocument.Package != (VersionPackage{Path: testPackagePath}) || !reflect.DeepEqual(versionDocument.Links, sourceManifest.Links) || !reflect.DeepEqual(versionDocument.Content, map[string]string{
		"api":               "./api.json",
		"documentation":     "./docs.md",
		"documentationHtml": "./docs.html",
	}) {
		t.Fatalf("unexpected version source/content: %#v", versionDocument)
	}
	if got := string(distribution.Files["modules/montferret/archive/versions/1.2.0/docs.md"]); got != documentation {
		t.Fatalf("documentation differs:\n%s", got)
	}
	if got := string(distribution.Files["modules/montferret/archive/versions/1.2.0/docs.html"]); !strings.Contains(got, `<h1 id="archive">Archive</h1>`) || !strings.Contains(got, "Pinned documentation.") {
		t.Fatalf("rendered documentation differs:\n%s", got)
	}

	var apiReference registryartifact.APIReference
	decodeDistributionJSON(t, distribution, "modules/montferret/archive/versions/1.2.0/api.json", &apiReference)
	wantSignature := registryartifact.APIFunctionSignature{
		Parameters:  []registryartifact.APIParameter{{Name: "data", Type: "String|Binary", Description: "Source content."}},
		Description: "Run processes source content.",
		Return:      &registryartifact.APIReturn{Type: "Object", Description: "Processed content."},
		Throws:      []registryartifact.APIThrownError{{Error: "ParseError", Description: "Source content is malformed."}},
		Deprecated:  "Use PARSE instead.",
	}
	if got := apiReference.Namespaces[0].Functions[0].Signatures[0]; !reflect.DeepEqual(got, wantSignature) {
		t.Fatalf("unexpected generated API signature: %#v", got)
	}

	if data := string(distribution.Files["modules/montferret/archive/versions/1.2.0/api.json"]); strings.Contains(data, "documentation") {
		t.Fatalf("API Reference contains the removed documentation field:\n%s", data)
	}

	if data := string(distribution.Files[versionPath]); strings.Contains(data, "archive/v1.2.0") || strings.Contains(data, sourceManifest.Documentation) || strings.Contains(data, documentation) {
		t.Fatalf("version document leaks publication or documentation data:\n%s", data)
	}
}

func assertCanonicalArtifactDocuments(t *testing.T, distribution *Distribution, parsers map[string]func([]byte) error) {
	t.Helper()

	for artifactPath, parse := range parsers {
		data, exists := distribution.Files[artifactPath]
		if !exists {
			t.Errorf("artifact %s was not generated", artifactPath)
			continue
		}

		if err := parse(data); err != nil {
			t.Errorf("artifact %s does not satisfy its portable contract: %v", artifactPath, err)
		}
	}
}

func TestGenerateDistributionDeterministicOrderingAndLatest(t *testing.T) {
	archive := distributionTestModule("montferret", "archive", []distributionTestVersion{
		{version: "1.1.0-beta.1", namespace: "ARCHIVE_BETA", description: "Beta.", ferret: ">=2.2.0-beta.1"},
		{version: "1.0.0", namespace: "ARCHIVE", description: "Stable.", ferret: ">=2.1.0"},
		{version: "1.0.0+build.2", namespace: "ARCHIVE_BUILD", description: "Build."},
	})
	browser := distributionTestModule("acme", "browser-tools", []distributionTestVersion{{
		version: "2.0.0-beta.2", namespace: "BROWSER", description: "Browser beta.", ferret: "^2.0.0",
	}})
	registry := &Registry{Modules: []*Module{archive, browser}}
	first, err := GenerateDistribution(registry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateDistribution(registry)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Files, second.Files) {
		t.Fatal("distribution generation is not deterministic")
	}

	var index ModuleIndex
	decodeDistributionJSON(t, first, "modules/index.json", &index)
	if len(index.Modules) != 2 || index.Modules[0].ID != "acme/browser-tools" || index.Modules[1].ID != "montferret/archive" {
		t.Fatalf("modules not sorted: %#v", index.Modules)
	}
	if index.Modules[0].Latest != "" || index.Modules[1].Latest != "1.0.0" {
		t.Fatalf("latest selection differs: %#v", index.Modules)
	}

	var browserDocument ModuleDocument
	decodeDistributionJSON(t, first, "modules/acme/browser-tools/index.json", &browserDocument)
	if browserDocument.Latest != "" || browserDocument.Description != "Browser beta." {
		t.Fatalf("prerelease-only metadata differs: %#v", browserDocument)
	}

	var archiveDocument ModuleDocument
	decodeDistributionJSON(t, first, "modules/montferret/archive/index.json", &archiveDocument)
	if archiveDocument.Latest != "1.0.0" || archiveDocument.Description != "Stable." {
		t.Fatalf("stable metadata differs: %#v", archiveDocument)
	}
	wantVersions := []string{"1.1.0-beta.1", "1.0.0", "1.0.0+build.2"}
	for index, want := range wantVersions {
		if archiveDocument.Versions[index].Version != want {
			t.Fatalf("version order differs: %#v", archiveDocument.Versions)
		}
	}

	var beta VersionDocument
	decodeDistributionJSON(t, first, "modules/montferret/archive/versions/1.1.0-beta.1/index.json", &beta)
	if beta.Namespace != "ARCHIVE_BETA" || beta.Ferret != ">=2.2.0-beta.1" || beta.Package.Path == "" || beta.Content["documentation"] != "./docs.md" || beta.Content["documentationHtml"] != "./docs.html" {
		t.Fatalf("version metadata differs: %#v", beta)
	}
	buildData := string(first.Files["modules/montferret/archive/versions/1.0.0+build.2/index.json"])
	if strings.Contains(buildData, "\"ferret\"") || strings.Contains(buildData, "\"links\"") {
		t.Fatalf("optional empty version fields were emitted:\n%s", buildData)
	}
}

func TestGenerateDistributionCategoryIndexes(t *testing.T) {
	archive := distributionTestModule("montferret", "archive", []distributionTestVersion{{
		version: "1.0.0", namespace: "ARCHIVE", description: "Archives.", categories: []string{"files", "data-formats", "files"},
	}})
	article := distributionTestModule("montferret", "article", []distributionTestVersion{{
		version: "1.1.0", namespace: "ARTICLE", description: "Articles.", categories: []string{"files"},
	}})
	browser := distributionTestModule("acme", "browser-tools", []distributionTestVersion{{
		version: "2.0.0-beta.1", namespace: "BROWSER", description: "Browser beta.", categories: []string{"web"},
	}})
	registry := &Registry{Modules: []*Module{article, browser, archive}}

	first, err := GenerateDistribution(registry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateDistribution(registry)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Files, second.Files) {
		t.Fatal("category generation is not deterministic")
	}

	var categoryIndex CategoryIndex
	decodeDistributionJSON(t, first, "categories.json", &categoryIndex)
	wantCategories := []CategoryIndexEntry{
		{ID: "data-formats", Name: "Data Formats", Count: 1, Href: "/categories/data-formats.json"},
		{ID: "files", Name: "Files", Count: 2, Href: "/categories/files.json"},
		{ID: "web", Name: "Web", Count: 1, Href: "/categories/web.json"},
	}
	if categoryIndex.SchemaVersion != 1 || !reflect.DeepEqual(categoryIndex.Categories, wantCategories) {
		t.Fatalf("unexpected category index: %#v", categoryIndex)
	}

	var moduleIndex ModuleIndex
	decodeDistributionJSON(t, first, "modules/index.json", &moduleIndex)
	entriesByID := make(map[string]ModuleIndexEntry, len(moduleIndex.Modules))
	for _, entry := range moduleIndex.Modules {
		entriesByID[entry.ID] = entry
	}

	var files CategoryDocument
	decodeDistributionJSON(t, first, "categories/files.json", &files)
	wantFileModules := []ModuleIndexEntry{
		entriesByID["montferret/archive"],
		entriesByID["montferret/article"],
	}
	if files.SchemaVersion != 1 || files.Category != (CategorySummary{ID: "files", Name: "Files"}) || !reflect.DeepEqual(files.Modules, wantFileModules) {
		t.Fatalf("unexpected files category: %#v", files)
	}
	if data := string(first.Files["categories/files.json"]); strings.Contains(data, "description") || strings.Contains(data, "namespace") || strings.Contains(data, "versions") {
		t.Fatalf("category listing embeds module details:\n%s", data)
	}

	var dataFormats CategoryDocument
	decodeDistributionJSON(t, first, "categories/data-formats.json", &dataFormats)
	if !reflect.DeepEqual(dataFormats.Modules, []ModuleIndexEntry{entriesByID["montferret/archive"]}) {
		t.Fatalf("module was not projected into every declared category: %#v", dataFormats)
	}

	var web CategoryDocument
	decodeDistributionJSON(t, first, "categories/web.json", &web)
	if len(web.Modules) != 1 || web.Modules[0].ID != "acme/browser-tools" || web.Modules[0].Latest != "" {
		t.Fatalf("prerelease-only category summary differs: %#v", web)
	}
}

func TestGenerateDistributionCategoriesFollowCurrentMetadataVersion(t *testing.T) {
	initial := distributionTestModule("montferret", "archive", []distributionTestVersion{
		{version: "2.0.0-beta.1", namespace: "ARCHIVE_NEXT", description: "Next.", categories: []string{"future"}},
		{version: "1.0.0", namespace: "ARCHIVE", description: "Stable.", categories: []string{"archives"}},
	})
	distribution, err := GenerateDistribution(&Registry{Modules: []*Module{initial}})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := distribution.Files["categories/archives.json"]; !exists {
		t.Fatal("stable metadata category was not generated")
	}
	if _, exists := distribution.Files["categories/future.json"]; exists {
		t.Fatal("newer prerelease category replaced stable metadata")
	}

	updated := distributionTestModule("montferret", "archive", []distributionTestVersion{
		{version: "2.0.0", namespace: "ARCHIVE", description: "Current.", categories: []string{"current"}},
		{version: "2.0.0-beta.1", namespace: "ARCHIVE_NEXT", description: "Next.", categories: []string{"future"}},
		{version: "1.0.0", namespace: "ARCHIVE", description: "Stable.", categories: []string{"archives"}},
	})
	distribution, err = GenerateDistribution(&Registry{Modules: []*Module{updated}})
	if err != nil {
		t.Fatal(err)
	}

	var categoryIndex CategoryIndex
	decodeDistributionJSON(t, distribution, "categories.json", &categoryIndex)
	if !reflect.DeepEqual(categoryIndex.Categories, []CategoryIndexEntry{{
		ID: "current", Name: "Current", Count: 1, Href: "/categories/current.json",
	}}) {
		t.Fatalf("category index did not follow updated metadata: %#v", categoryIndex)
	}
	for _, stale := range []string{"categories/archives.json", "categories/future.json"} {
		if _, exists := distribution.Files[stale]; exists {
			t.Fatalf("stale metadata category %s was generated", stale)
		}
	}
}

func TestGenerateDistributionRejectsInvalidCategoryIDs(t *testing.T) {
	for _, categoryID := range []string{
		"",
		"../legacy",
		"data/formats",
		`data\formats`,
		"data formats",
		"Data-Formats",
		"-data",
		"data-",
		"data--formats",
	} {
		t.Run(strings.ReplaceAll(categoryID, "/", "_"), func(t *testing.T) {
			module := distributionTestModule("montferret", "archive", []distributionTestVersion{{
				version: "1.0.0", namespace: "ARCHIVE", description: "Archives.", categories: []string{categoryID},
			}})

			_, err := GenerateDistribution(&Registry{Modules: []*Module{module}})
			if err == nil || !strings.Contains(err.Error(), "montferret/archive@1.0.0") || !strings.Contains(err.Error(), categoryIDPatternText) {
				t.Fatalf("expected contextual category validation error for %q, got %v", categoryID, err)
			}
		})
	}
}

func TestGenerateDistributionRejectsCaseVariantIdentities(t *testing.T) {
	canonical := distributionTestModule("montferret", "archive", []distributionTestVersion{{
		version: "1.0.0", namespace: "ARCHIVE", description: "Archives.",
	}})

	for _, test := range []struct {
		name   string
		owner  string
		module string
	}{
		{name: "uppercase owner", owner: "MONTFERRET", module: "archive"},
		{name: "mixed-case owner", owner: "MontFerret", module: "archive"},
		{name: "uppercase module", owner: "montferret", module: "ARCHIVE"},
		{name: "mixed-case module", owner: "montferret", module: "Archive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			variant := distributionTestModule(test.owner, test.module, []distributionTestVersion{{
				version: "1.0.0", namespace: "ARCHIVE", description: "Archives.",
			}})

			distribution, err := GenerateDistribution(&Registry{Modules: []*Module{canonical, variant}})
			if err == nil {
				t.Fatal("expected case-variant identity to be rejected")
			}
			if distribution != nil {
				t.Fatalf("invalid registry returned generated artifacts: %#v", distribution.Files)
			}
		})
	}
}

func TestDistributionRootAndEmptyIndexes(t *testing.T) {
	distribution, err := GenerateDistribution(&Registry{})
	if err != nil {
		t.Fatal(err)
	}

	wantRoot := "{\n  \"schemaVersion\": 1,\n  \"artifacts\": {\n    \"categories\": \"/categories.json\",\n    \"modules\": \"/modules/index.json\",\n    \"plugins\": \"/plugins/index.json\"\n  }\n}\n"
	if got := string(distribution.Files["index.json"]); got != wantRoot {
		t.Fatalf("root index differs:\n%s", got)
	}
	wantCategories := "{\n  \"schemaVersion\": 1,\n  \"categories\": []\n}\n"
	if got := string(distribution.Files["categories.json"]); got != wantCategories {
		t.Fatalf("category index differs:\n%s", got)
	}
	wantModules := "{\n  \"schemaVersion\": 1,\n  \"modules\": []\n}\n"
	if got := string(distribution.Files["modules/index.json"]); got != wantModules {
		t.Fatalf("module index differs:\n%s", got)
	}
	wantPlugins := "{\n  \"schemaVersion\": 1,\n  \"plugins\": []\n}\n"
	if got := string(distribution.Files["plugins/index.json"]); got != wantPlugins {
		t.Fatalf("plugin index differs:\n%s", got)
	}
}

func TestDistributionWriteReplacementAndVerification(t *testing.T) {
	root := t.TempDir()
	distribution, err := GenerateDistribution(&Registry{})
	if err != nil {
		t.Fatal(err)
	}

	if err := VerifyDistribution(root, distribution); err == nil || !strings.Contains(err.Error(), "dist") {
		t.Fatalf("expected missing distribution error, got %v", err)
	}
	if err := WriteDistribution(root, distribution); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDistribution(root, distribution); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "catalog")); !os.IsNotExist(err) {
		t.Fatalf("legacy catalog directory was created: %v", err)
	}
	pluginIndexPath := filepath.Join(root, "dist", "plugins", "index.json")
	if err := os.Remove(pluginIndexPath); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDistribution(root, distribution); err == nil || !strings.Contains(err.Error(), "dist/plugins/index.json is missing") {
		t.Fatalf("expected missing file error, got %v", err)
	}
	if err := WriteDistribution(root, distribution); err != nil {
		t.Fatal(err)
	}

	rootIndexPath := filepath.Join(root, "dist", "index.json")
	if err := os.WriteFile(rootIndexPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDistribution(root, distribution); err == nil || !strings.Contains(err.Error(), "dist/index.json is stale") {
		t.Fatalf("expected stale distribution error, got %v", err)
	}

	if err := WriteDistribution(root, distribution); err != nil {
		t.Fatal(err)
	}
	extraPath := filepath.Join(root, "dist", "categories", "stale.json")
	if err := os.MkdirAll(filepath.Dir(extraPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extraPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDistribution(root, distribution); err == nil || !strings.Contains(err.Error(), "dist/categories/stale.json is unexpected") {
		t.Fatalf("expected unexpected file error, got %v", err)
	}

	if err := WriteDistribution(root, distribution); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(extraPath); !os.IsNotExist(err) {
		t.Fatalf("stale output survived full replacement: %v", err)
	}
	if err := VerifyDistribution(root, distribution); err != nil {
		t.Fatal(err)
	}
}

func TestDistributionReplacementRemovesStaleCategoryArtifacts(t *testing.T) {
	root := t.TempDir()
	legacy := distributionTestModule("montferret", "archive", []distributionTestVersion{{
		version: "1.0.0", namespace: "ARCHIVE", description: "Archives.", categories: []string{"legacy"},
	}})
	initial, err := GenerateDistribution(&Registry{Modules: []*Module{legacy}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDistribution(root, initial); err != nil {
		t.Fatal(err)
	}

	legacyPath := filepath.Join(root, "dist", "categories", "legacy.json")
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("initial category artifact was not written: %v", err)
	}

	current := distributionTestModule("montferret", "archive", []distributionTestVersion{{
		version: "1.1.0", namespace: "ARCHIVE", description: "Archives.", categories: []string{"current"},
	}})
	replacement, err := GenerateDistribution(&Registry{Modules: []*Module{current}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDistribution(root, replacement); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("stale category artifact survived replacement: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "categories", "current.json")); err != nil {
		t.Fatalf("replacement category artifact was not written: %v", err)
	}
	if err := VerifyDistribution(root, replacement); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateDistributionRequiresResolvedDocumentation(t *testing.T) {
	module := distributionTestModule("montferret", "archive", []distributionTestVersion{{
		version: "1.0.0", namespace: "ARCHIVE", description: "Archives.",
	}})
	module.Versions[0].Documentation = nil

	_, err := GenerateDistribution(&Registry{Modules: []*Module{module}})
	if err == nil || !strings.Contains(err.Error(), "documentation has not been resolved") {
		t.Fatalf("expected unresolved documentation error, got %v", err)
	}
}

func TestGenerateDistributionRequiresResolvedPackage(t *testing.T) {
	module := distributionTestModule("montferret", "archive", []distributionTestVersion{{
		version: "1.0.0", namespace: "ARCHIVE", description: "Archives.",
	}})
	module.Versions[0].PackagePath = ""

	_, err := GenerateDistribution(&Registry{Modules: []*Module{module}})
	if err == nil || !strings.Contains(err.Error(), "package has not been resolved") {
		t.Fatalf("expected unresolved package error, got %v", err)
	}
}

func TestGenerateDistributionRequiresResolvedAPIReference(t *testing.T) {
	module := distributionTestModule("montferret", "archive", []distributionTestVersion{{
		version: "1.0.0", namespace: "ARCHIVE", description: "Archives.",
	}})
	module.Versions[0].API = nil

	_, err := GenerateDistribution(&Registry{Modules: []*Module{module}})
	if err == nil || !strings.Contains(err.Error(), "API Reference has not been resolved") {
		t.Fatalf("expected unresolved API Reference error, got %v", err)
	}
}

func TestGenerateDistributionRequiresPublicationStamp(t *testing.T) {
	module := distributionTestModule("montferret", "archive", []distributionTestVersion{{
		version: "1.0.0", namespace: "ARCHIVE", description: "Archives.",
	}})
	module.Versions[0].Record.PublishedAt = nil

	_, err := GenerateDistribution(&Registry{Modules: []*Module{module}})
	if err == nil || !strings.Contains(err.Error(), "has not been publication-stamped") {
		t.Fatalf("expected missing publication timestamp error, got %v", err)
	}
}

type distributionTestVersion struct {
	version     string
	namespace   string
	description string
	ferret      string
	categories  []string
}

func distributionTestModule(owner, name string, fixtures []distributionTestVersion) *Module {
	versions := make([]*Version, 0, len(fixtures))
	for index, fixture := range fixtures {
		manifest := testModuleManifest(owner+"/"+name, fixture.namespace, fixture.version, fixture.description)
		manifest.Categories = append([]string(nil), fixture.categories...)
		if fixture.ferret != "" {
			manifest.Compatibility = &modulemanifest.Compatibility{Ferret: fixture.ferret}
		}
		packagePath := "example.org/fixtures/" + name
		major, _, _ := strings.Cut(fixture.version, ".")
		if major != "0" && major != "1" {
			packagePath += "/v" + major
		}

		versions = append(versions, &Version{
			Record:        stampedTestVersion(fixture.version, "v"+fixture.version, testCommit[:39]+string(rune('0'+index))),
			Manifest:      manifest,
			PackagePath:   packagePath,
			Documentation: []byte("# " + fixture.version + "\n"),
			API: &registryartifact.APIReference{
				SchemaVersion: registryartifact.SchemaVersion,
				ID:            owner + "/" + name,
				Version:       fixture.version,
				Namespaces: []registryartifact.APINamespace{{
					Name: fixture.namespace,
					Functions: []registryartifact.APIFunction{{
						Name:       "RUN",
						Signatures: []registryartifact.APIFunctionSignature{{Parameters: []registryartifact.APIParameter{}}},
					}},
				}},
			},
		})
	}

	return &Module{Manifest: testRegistryManifest(owner, name), Versions: versions}
}

func decodeDistributionJSON(t *testing.T, distribution *Distribution, relativePath string, target any) {
	t.Helper()
	data, exists := distribution.Files[relativePath]
	if !exists {
		t.Fatalf("distribution file %s is missing", relativePath)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", relativePath, err)
	}
}
