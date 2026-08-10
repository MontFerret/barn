package barn

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/MontFerret/specs/pkg/api"
)

func TestBuildDistributionIncrementallyReusesImmutableReleases(t *testing.T) {
	root := newGenerationRepository(t)
	writeRegistryRecord(t, root, "acme", "archive", testRegistryManifest("acme", "archive"), stampedTestVersion("1.0.0", "v1.0.0", testCommit))
	base := commitGenerationRepository(t, root, "register v1")

	fullInspector := &recordingInspector{categories: map[string][]string{"1.0.0": {"files"}}}
	full, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		SourceCommit: base,
		Mode:         GenerationModeFull,
		Inspector:    fullInspector,
	})
	if err != nil {
		t.Fatal(err)
	}

	previous := filepath.Join(t.TempDir(), "published")
	if err := WriteDistributionPath(previous, full.Distribution); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(previous, ".nojekyll"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(previous, "CNAME"), []byte("registry.ferretlang.org\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeRegistryRecord(
		t,
		root,
		"acme",
		"archive",
		testRegistryManifest("acme", "archive"),
		stampedTestVersion("1.0.0", "v1.0.0", testCommit),
		stampedTestVersion("2.0.0", "v2.0.0", strings.Repeat("a", 40)),
	)
	target := commitGenerationRepository(t, root, "register v2")

	incrementalInspector := &recordingInspector{categories: map[string][]string{"2.0.0": {"database"}}}
	incremental, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		Previous:     previous,
		SourceCommit: target,
		Mode:         GenerationModeAuto,
		Inspector:    incrementalInspector,
	})
	if err != nil {
		t.Fatal(err)
	}

	if incremental.Mode != GenerationModeIncremental {
		t.Fatalf("generation mode is %s", incremental.Mode)
	}

	if !reflect.DeepEqual(incrementalInspector.calls, []string{"acme/archive@2.0.0"}) {
		t.Fatalf("resolved releases differ: %v", incrementalInspector.calls)
	}

	oldPrefix := "modules/acme/archive/versions/1.0.0/"
	for relative, data := range full.Distribution.Files {
		if strings.HasPrefix(relative, oldPrefix) && !bytes.Equal(data, incremental.Distribution.Files[relative]) {
			t.Fatalf("unchanged release artifact %s was regenerated", relative)
		}
	}

	var rootIndex RootIndex
	decodeDistributionJSON(t, incremental.Distribution, "index.json", &rootIndex)
	if rootIndex.Source.Commit != target || rootIndex.Source.Commit == base {
		t.Fatalf("root source commit is %q, want %q", rootIndex.Source.Commit, target)
	}

	var module ModuleDocument
	decodeDistributionJSON(t, incremental.Distribution, "modules/acme/archive/index.json", &module)
	if module.Latest != "2.0.0" || module.Description != "Release 2.0.0" {
		t.Fatalf("module projection was not updated: %#v", module)
	}

	var categories CategoryIndex
	decodeDistributionJSON(t, incremental.Distribution, "categories.json", &categories)
	if len(categories.Categories) != 1 || categories.Categories[0].ID != "database" {
		t.Fatalf("category projection was not updated: %#v", categories)
	}

	if _, exists := incremental.Distribution.Files["categories/files.json"]; exists {
		t.Fatal("stale category projection was preserved")
	}
}

func TestBuildDistributionAutoUsesFullModeForNonRegistryChanges(t *testing.T) {
	root := newGenerationRepository(t)
	writeRegistryRecord(t, root, "acme", "archive", testRegistryManifest("acme", "archive"), stampedTestVersion("1.0.0", "v1.0.0", testCommit))
	base := commitGenerationRepository(t, root, "register module")

	initial, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		SourceCommit: base,
		Inspector:    &recordingInspector{},
	})
	if err != nil {
		t.Fatal(err)
	}

	previous := filepath.Join(t.TempDir(), "published")
	if err := WriteDistributionPath(previous, initial.Distribution); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("generator documentation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := commitGenerationRepository(t, root, "update documentation")

	inspector := &recordingInspector{}
	result, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		Previous:     previous,
		SourceCommit: target,
		Mode:         GenerationModeAuto,
		Inspector:    inspector,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Mode != GenerationModeFull || !reflect.DeepEqual(inspector.calls, []string{"acme/archive@1.0.0"}) {
		t.Fatalf("auto generation did not perform a full rebuild: mode=%s calls=%v", result.Mode, inspector.calls)
	}
}

func TestBuildDistributionManifestChangeReenrichesEveryRelease(t *testing.T) {
	root := newGenerationRepository(t)
	writeRegistryRecord(
		t,
		root,
		"acme",
		"archive",
		testRegistryManifest("acme", "archive"),
		stampedTestVersion("1.0.0", "v1.0.0", testCommit),
		stampedTestVersion("2.0.0", "v2.0.0", strings.Repeat("a", 40)),
	)
	base := commitGenerationRepository(t, root, "register releases")

	initial, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		SourceCommit: base,
		Inspector:    &recordingInspector{},
	})
	if err != nil {
		t.Fatal(err)
	}

	previous := filepath.Join(t.TempDir(), "published")
	if err := WriteDistributionPath(previous, initial.Distribution); err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "acme", "archive", "manifest.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(manifestPath, append(manifest, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	target := commitGenerationRepository(t, root, "reformat module manifest")

	inspector := &recordingInspector{}
	result, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		Previous:     previous,
		SourceCommit: target,
		Mode:         GenerationModeIncremental,
		Inspector:    inspector,
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"acme/archive@1.0.0", "acme/archive@2.0.0"}
	if result.Mode != GenerationModeIncremental || !reflect.DeepEqual(inspector.calls, want) {
		t.Fatalf("manifest change resolved %v in mode %s, want %v", inspector.calls, result.Mode, want)
	}
}

func TestBuildDistributionIncrementalPrereleasePreservesStableMetadata(t *testing.T) {
	root := newGenerationRepository(t)
	writeRegistryRecord(t, root, "acme", "archive", testRegistryManifest("acme", "archive"), stampedTestVersion("1.0.0", "v1.0.0", testCommit))
	base := commitGenerationRepository(t, root, "register stable release")

	initial, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		SourceCommit: base,
		Inspector:    &recordingInspector{categories: map[string][]string{"1.0.0": {"files"}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	previous := filepath.Join(t.TempDir(), "published")
	if err := WriteDistributionPath(previous, initial.Distribution); err != nil {
		t.Fatal(err)
	}

	writeRegistryRecord(
		t,
		root,
		"acme",
		"archive",
		testRegistryManifest("acme", "archive"),
		stampedTestVersion("1.0.0", "v1.0.0", testCommit),
		stampedTestVersion("1.1.0-beta.1", "v1.1.0-beta.1", strings.Repeat("b", 40)),
	)
	target := commitGenerationRepository(t, root, "register prerelease")

	inspector := &recordingInspector{categories: map[string][]string{"1.1.0-beta.1": {"database"}}}
	result, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		Previous:     previous,
		SourceCommit: target,
		Mode:         GenerationModeIncremental,
		Inspector:    inspector,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(inspector.calls, []string{"acme/archive@1.1.0-beta.1"}) {
		t.Fatalf("resolved releases differ: %v", inspector.calls)
	}

	var module ModuleDocument
	decodeDistributionJSON(t, result.Distribution, "modules/acme/archive/index.json", &module)
	if module.Latest != "1.0.0" || module.Description != "Release 1.0.0" || len(module.Versions) != 2 {
		t.Fatalf("prerelease changed stable module metadata: %#v", module)
	}

	var categories CategoryIndex
	decodeDistributionJSON(t, result.Distribution, "categories.json", &categories)
	if len(categories.Categories) != 1 || categories.Categories[0].ID != "files" {
		t.Fatalf("prerelease changed stable category projection: %#v", categories)
	}
}

func TestBuildDistributionRejectsCachedImmutableMutation(t *testing.T) {
	root := newGenerationRepository(t)
	manifest := testRegistryManifest("acme", "archive")
	writeRegistryRecord(t, root, "acme", "archive", manifest, stampedTestVersion("1.0.0", "v1.0.0", testCommit))
	base := commitGenerationRepository(t, root, "register module")

	initial, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		SourceCommit: base,
		Inspector:    &recordingInspector{},
	})
	if err != nil {
		t.Fatal(err)
	}

	previous := filepath.Join(t.TempDir(), "published")
	if err := WriteDistributionPath(previous, initial.Distribution); err != nil {
		t.Fatal(err)
	}

	writeRegistryRecord(t, root, "acme", "archive", manifest, stampedTestVersion("1.0.0", "v1.0.0", strings.Repeat("b", 40)))
	target := commitGenerationRepository(t, root, "mutate release")

	_, err = BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		Previous:     previous,
		SourceCommit: target,
		Mode:         GenerationModeIncremental,
		Inspector:    &recordingInspector{},
	})
	if err == nil || !strings.Contains(err.Error(), "does not match canonical source") {
		t.Fatalf("expected immutable mutation failure, got %v", err)
	}
}

func TestBuildDistributionRejectsCachedImmutableDeletion(t *testing.T) {
	root := newGenerationRepository(t)
	writeRegistryRecord(
		t,
		root,
		"acme",
		"archive",
		testRegistryManifest("acme", "archive"),
		stampedTestVersion("1.0.0", "v1.0.0", testCommit),
		stampedTestVersion("2.0.0", "v2.0.0", strings.Repeat("a", 40)),
	)
	base := commitGenerationRepository(t, root, "register module")

	initial, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		SourceCommit: base,
		Inspector:    &recordingInspector{},
	})
	if err != nil {
		t.Fatal(err)
	}

	previous := filepath.Join(t.TempDir(), "published")
	if err := WriteDistributionPath(previous, initial.Distribution); err != nil {
		t.Fatal(err)
	}

	versionPath := filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "acme", "archive", "versions", "v1.0.0.json")
	if err := os.Remove(versionPath); err != nil {
		t.Fatal(err)
	}
	target := commitGenerationRepository(t, root, "delete release")

	_, err = BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		Previous:     previous,
		SourceCommit: target,
		Mode:         GenerationModeIncremental,
		Inspector:    &recordingInspector{},
	})
	if err == nil || !strings.Contains(err.Error(), "was deleted") {
		t.Fatalf("expected immutable deletion failure, got %v", err)
	}
}

func TestBuildDistributionFailureDoesNotReplacePublishedTree(t *testing.T) {
	root := newGenerationRepository(t)
	writeRegistryRecord(t, root, "acme", "archive", testRegistryManifest("acme", "archive"), stampedTestVersion("1.0.0", "v1.0.0", testCommit))
	base := commitGenerationRepository(t, root, "register v1")

	initial, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		SourceCommit: base,
		Inspector:    &recordingInspector{},
	})
	if err != nil {
		t.Fatal(err)
	}

	previous := filepath.Join(t.TempDir(), "published")
	if err := WriteDistributionPath(previous, initial.Distribution); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(previous, "index.json"))
	if err != nil {
		t.Fatal(err)
	}

	writeRegistryRecord(
		t,
		root,
		"acme",
		"archive",
		testRegistryManifest("acme", "archive"),
		stampedTestVersion("1.0.0", "v1.0.0", testCommit),
		stampedTestVersion("2.0.0", "v2.0.0", strings.Repeat("c", 40)),
	)
	target := commitGenerationRepository(t, root, "register v2")

	_, err = BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		Previous:     previous,
		SourceCommit: target,
		Mode:         GenerationModeIncremental,
		Inspector:    &recordingInspector{err: errors.New("enrichment failed")},
	})
	if err == nil || !strings.Contains(err.Error(), "enrichment failed") {
		t.Fatalf("expected enrichment failure, got %v", err)
	}

	after, err := os.ReadFile(filepath.Join(previous, "index.json"))
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(before, after) {
		t.Fatal("failed generation modified the published tree")
	}
}

func TestBuildDistributionRejectsMalformedAndDivergentPublishedState(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		root := newGenerationRepository(t)
		writeRegistryRecord(t, root, "acme", "archive", testRegistryManifest("acme", "archive"), stampedTestVersion("1.0.0", "v1.0.0", testCommit))
		commit := commitGenerationRepository(t, root, "register module")
		result, err := BuildDistribution(context.Background(), GenerationOptions{
			Root:         root,
			SourceCommit: commit,
			Inspector:    &recordingInspector{},
		})
		if err != nil {
			t.Fatal(err)
		}

		previous := filepath.Join(t.TempDir(), "published")
		if err := WriteDistributionPath(previous, result.Distribution); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(previous, "index.json"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err = BuildDistribution(context.Background(), GenerationOptions{
			Root:         root,
			Previous:     previous,
			SourceCommit: commit,
			Mode:         GenerationModeAuto,
			Inspector:    &recordingInspector{},
		})
		if err == nil || !strings.Contains(err.Error(), "parse published index.json") {
			t.Fatalf("expected malformed cache failure, got %v", err)
		}
	})

	t.Run("non-ancestor", func(t *testing.T) {
		root := newGenerationRepository(t)
		runTestGit(t, root, "commit", "--allow-empty", "-m", "initial")
		initial := strings.TrimSpace(runTestGit(t, root, "rev-parse", "HEAD"))
		writeRegistryRecord(t, root, "acme", "archive", testRegistryManifest("acme", "archive"), stampedTestVersion("1.0.0", "v1.0.0", testCommit))
		base := commitGenerationRepository(t, root, "published branch")
		result, err := BuildDistribution(context.Background(), GenerationOptions{
			Root:         root,
			SourceCommit: base,
			Inspector:    &recordingInspector{},
		})
		if err != nil {
			t.Fatal(err)
		}

		previous := filepath.Join(t.TempDir(), "published")
		if err := WriteDistributionPath(previous, result.Distribution); err != nil {
			t.Fatal(err)
		}

		runTestGit(t, root, "checkout", "--detach", initial)
		writeRegistryRecord(t, root, "acme", "archive", testRegistryManifest("acme", "archive"), stampedTestVersion("1.0.0", "v1.0.0", testCommit))
		target := commitGenerationRepository(t, root, "divergent branch")

		_, err = BuildDistribution(context.Background(), GenerationOptions{
			Root:         root,
			Previous:     previous,
			SourceCommit: target,
			Mode:         GenerationModeAuto,
			Inspector:    &recordingInspector{},
		})
		if err == nil || !strings.Contains(err.Error(), "is not an ancestor") {
			t.Fatalf("expected divergent cache failure, got %v", err)
		}
	})
}

func TestBuildDistributionRejectsUnsafeAndIncompletePublishedState(t *testing.T) {
	root := newGenerationRepository(t)
	writeRegistryRecord(t, root, "acme", "archive", testRegistryManifest("acme", "archive"), stampedTestVersion("1.0.0", "v1.0.0", testCommit))
	commit := commitGenerationRepository(t, root, "register module")
	result, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		SourceCommit: commit,
		Inspector:    &recordingInspector{},
	})
	if err != nil {
		t.Fatal(err)
	}

	materialize := func(t *testing.T) string {
		t.Helper()
		previous := filepath.Join(t.TempDir(), "published")
		if err := WriteDistributionPath(previous, result.Distribution); err != nil {
			t.Fatal(err)
		}

		return previous
	}

	t.Run("symbolic link", func(t *testing.T) {
		previous := materialize(t)
		if err := os.Symlink("index.json", filepath.Join(previous, "unsafe")); err != nil {
			t.Fatal(err)
		}

		_, err := BuildDistribution(context.Background(), GenerationOptions{
			Root: root, Previous: previous, SourceCommit: commit, Mode: GenerationModeAuto, Inspector: &recordingInspector{},
		})
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("expected symbolic-link failure, got %v", err)
		}
	})

	t.Run("missing release file", func(t *testing.T) {
		previous := materialize(t)
		if err := os.Remove(filepath.Join(previous, "modules", "acme", "archive", "versions", "1.0.0", "api.json")); err != nil {
			t.Fatal(err)
		}

		_, err := BuildDistribution(context.Background(), GenerationOptions{
			Root: root, Previous: previous, SourceCommit: commit, Mode: GenerationModeAuto, Inspector: &recordingInspector{},
		})
		if err == nil || !strings.Contains(err.Error(), "api.json is missing") {
			t.Fatalf("expected missing-file failure, got %v", err)
		}
	})

	t.Run("unexpected file", func(t *testing.T) {
		previous := materialize(t)
		if err := os.WriteFile(filepath.Join(previous, "unexpected.json"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := BuildDistribution(context.Background(), GenerationOptions{
			Root: root, Previous: previous, SourceCommit: commit, Mode: GenerationModeAuto, Inspector: &recordingInspector{},
		})
		if err == nil || !strings.Contains(err.Error(), "unexpected.json is unexpected") {
			t.Fatalf("expected unexpected-file failure, got %v", err)
		}
	})
}

func TestBuildDistributionFullModeIgnoresCorruptPreviousState(t *testing.T) {
	root := newGenerationRepository(t)
	writeRegistryRecord(t, root, "acme", "archive", testRegistryManifest("acme", "archive"), stampedTestVersion("1.0.0", "v1.0.0", testCommit))
	commit := commitGenerationRepository(t, root, "register module")
	previous := filepath.Join(t.TempDir(), "published")
	if err := os.MkdirAll(previous, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previous, "index.json"), []byte("corrupt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inspector := &recordingInspector{}
	result, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		Previous:     previous,
		SourceCommit: commit,
		Mode:         GenerationModeFull,
		Inspector:    inspector,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Mode != GenerationModeFull || !reflect.DeepEqual(inspector.calls, []string{"acme/archive@1.0.0"}) {
		t.Fatalf("full recovery mode did not rebuild every release: mode=%s calls=%v", result.Mode, inspector.calls)
	}
}

func TestBuildDistributionAutoRebuildsLegacyRoot(t *testing.T) {
	root := newGenerationRepository(t)
	writeRegistryRecord(t, root, "acme", "archive", testRegistryManifest("acme", "archive"), stampedTestVersion("1.0.0", "v1.0.0", testCommit))
	commit := commitGenerationRepository(t, root, "register module")
	initial, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		SourceCommit: commit,
		Inspector:    &recordingInspector{},
	})
	if err != nil {
		t.Fatal(err)
	}

	previous := filepath.Join(t.TempDir(), "published")
	if err := WriteDistributionPath(previous, initial.Distribution); err != nil {
		t.Fatal(err)
	}

	legacyRoot := "{\n  \"schemaVersion\": 1,\n  \"artifacts\": {\n    \"categories\": \"/categories.json\",\n    \"modules\": \"/modules/index.json\",\n    \"plugins\": \"/plugins/index.json\"\n  }\n}\n"
	if err := os.WriteFile(filepath.Join(previous, "index.json"), []byte(legacyRoot), 0o644); err != nil {
		t.Fatal(err)
	}

	inspector := &recordingInspector{}
	result, err := BuildDistribution(context.Background(), GenerationOptions{
		Root:         root,
		Previous:     previous,
		SourceCommit: commit,
		Mode:         GenerationModeAuto,
		Inspector:    inspector,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Mode != GenerationModeFull || !reflect.DeepEqual(inspector.calls, []string{"acme/archive@1.0.0"}) {
		t.Fatalf("legacy root did not select full generation: mode=%s calls=%v", result.Mode, inspector.calls)
	}
}

type recordingInspector struct {
	calls      []string
	categories map[string][]string
	err        error
}

func (inspector *recordingInspector) Resolve(_ context.Context, registry *Registry) error {
	if inspector.err != nil {
		return inspector.err
	}

	for _, module := range registry.Modules {
		for _, version := range module.Versions {
			inspector.calls = append(inspector.calls, releaseKey(module.ID(), version.Record.Version))
			manifest := testModuleManifest(module.ID(), "ARCHIVE", version.Record.Version, "Release "+version.Record.Version)
			manifest.Categories = append([]string(nil), inspector.categories[version.Record.Version]...)
			version.Manifest = manifest
			version.PackagePath = "example.org/" + module.ID()
			if strings.HasPrefix(version.Record.Version, "2.") {
				version.PackagePath += "/v2"
			}
			version.Documentation = []byte("# " + version.Record.Version + "\n")
			version.API = &api.Reference{
				SchemaVersion: api.SchemaVersion,
				ID:            module.ID(),
				Version:       version.Record.Version,
				Namespaces:    make([]api.Namespace, 0),
			}
		}
	}

	sort.Strings(inspector.calls)

	return nil
}

func newGenerationRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runTestGit(t, root, "init")
	runTestGit(t, root, "config", "user.name", "Barn Tests")
	runTestGit(t, root, "config", "user.email", "barn-tests@example.org")

	return root
}

func commitGenerationRepository(t *testing.T, root, message string) string {
	t.Helper()
	runTestGit(t, root, "add", ".")
	runTestGit(t, root, "commit", "-m", message)

	return strings.TrimSpace(runTestGit(t, root, "rev-parse", "HEAD"))
}

var _ Inspector = (*recordingInspector)(nil)
