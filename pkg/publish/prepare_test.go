package publish

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MontFerret/barn/internal/barn"
	"github.com/MontFerret/barn/internal/barn/apiref"
	registryclient "github.com/MontFerret/barn/pkg/registry"
	modulemanifest "github.com/MontFerret/specs/pkg/module"
	registryspec "github.com/MontFerret/specs/pkg/registry"
	registryartifact "github.com/MontFerret/specs/pkg/registry/artifact"
	"github.com/MontFerret/specs/pkg/validation"
)

const (
	testModuleID   = "acme/archive"
	testRepository = "https://example.com/acme/archive.git"
	testSourcePath = "modules/archive"
)

func TestPrepareNewModuleDeterministicAndBarnCompatible(t *testing.T) {
	directory := t.TempDir()
	writeManifest(t, directory, testModuleID, "1.2.3", testRepository, testSourcePath)

	inspector := &fakeInspector{release: &barn.ResolvedRelease{Commit: strings.Repeat("a", 40)}}
	reader := &fakeRegistry{moduleErr: fmt.Errorf("%w: %s", registryclient.ErrModuleNotFound, testModuleID)}
	request := Request{Directory: directory, Tag: "modules/archive/v1.2.3"}

	first, err := prepare(context.Background(), request, reader, inspector)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepare(context.Background(), request, reader, inspector)
	if err != nil {
		t.Fatal(err)
	}

	if first.Kind != NewModule || first.Module.Owner != "acme" || first.Module.Name != "archive" || first.Version.Version != "1.2.3" || first.Version.Commit != strings.Repeat("a", 40) {
		t.Fatalf("unexpected result: %#v", first)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("preparation is not deterministic:\nfirst: %#v\nsecond: %#v", first, second)
	}

	wantPaths := []string{
		"registry/modules/acme/archive/manifest.json",
		"registry/modules/acme/archive/versions/v1.2.3.json",
	}
	if got := []string{first.Files[0].Path, first.Files[1].Path}; !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("unexpected prepared paths: %#v", got)
	}
	if _, err := registryspec.ParseModuleManifest(first.Files[0].Content); err != nil {
		t.Fatalf("prepared module record is invalid: %v", err)
	}
	if _, err := registryspec.ParseVersionRecord(first.Files[1].Content); err != nil {
		t.Fatalf("prepared version record is invalid: %v", err)
	}
	if first.Version.PublishedAt != nil || strings.Contains(string(first.Files[1].Content), "publishedAt") {
		t.Fatalf("prepared submission was publication-stamped: %s", first.Files[1].Content)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "registry", "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range first.Files {
		filePath := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filePath, file.Content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	loaded, err := barn.Load(root)
	if err != nil {
		t.Fatalf("Barn rejected prepared files: %v", err)
	}
	if len(loaded.Modules) != 1 || loaded.Modules[0].ID() != testModuleID || len(loaded.Modules[0].Versions) != 1 {
		t.Fatalf("unexpected loaded submission: %#v", loaded)
	}
}

func TestPrepareAdditionalVersion(t *testing.T) {
	directory := t.TempDir()
	writeManifest(t, directory, testModuleID, "1.1.0", testRepository, testSourcePath)

	reader := existingRegistry("1.0.0", registryclient.Source{Repository: testRepository, Path: testSourcePath, Commit: strings.Repeat("b", 40)})
	result, err := prepare(
		context.Background(),
		Request{Directory: directory, Tag: "v1.1.0"},
		reader,
		&fakeInspector{release: &barn.ResolvedRelease{Commit: strings.Repeat("c", 40)}},
	)
	if err != nil {
		t.Fatal(err)
	}

	if result.Kind != NewVersion || len(result.Files) != 1 || result.Files[0].Path != "registry/modules/acme/archive/versions/v1.1.0.json" {
		t.Fatalf("unexpected additional-version result: %#v", result)
	}
}

func TestPrepareClassificationErrors(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		reader  registryReader
		target  error
	}{
		{
			name:    "already published",
			version: "1.0.0",
			reader:  existingRegistry("1.0.0", registryclient.Source{Repository: testRepository, Path: testSourcePath}),
			target:  ErrVersionAlreadyPublished,
		},
		{
			name:    "source changed",
			version: "1.1.0",
			reader:  existingRegistry("1.0.0", registryclient.Source{Repository: "https://example.com/other.git"}),
			target:  ErrSourceMismatch,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			writeManifest(t, directory, testModuleID, test.version, testRepository, testSourcePath)
			inspector := &fakeInspector{release: &barn.ResolvedRelease{Commit: strings.Repeat("a", 40)}}

			_, err := prepare(context.Background(), Request{Directory: directory, Tag: "v" + test.version}, test.reader, inspector)
			if !errors.Is(err, test.target) {
				t.Fatalf("expected %v, got %v", test.target, err)
			}
			if inspector.calls != 0 {
				t.Fatal("Git inspection ran after registry classification failed")
			}

			var preparationErr *Error
			if !errors.As(err, &preparationErr) || preparationErr.Stage != StageRegistry {
				t.Fatalf("expected registry-stage error, got %v", err)
			}
		})
	}
}

func TestPrepareValidatesManifestRequestAndGitFailures(t *testing.T) {
	t.Run("invalid manifest", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, modulemanifest.ManifestFilename), []byte("name: ["), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := prepare(context.Background(), Request{Directory: directory, Tag: "v1.0.0"}, &fakeRegistry{}, &fakeInspector{})
		assertStage(t, err, StageManifest)
	})

	t.Run("repository required", func(t *testing.T) {
		directory := t.TempDir()
		writeManifest(t, directory, testModuleID, "1.0.0", "", "")

		_, err := prepare(context.Background(), Request{Directory: directory, Tag: "v1.0.0"}, &fakeRegistry{}, &fakeInspector{})
		if !errors.Is(err, ErrRepositoryRequired) {
			t.Fatalf("expected repository-required error, got %v", err)
		}
		assertStage(t, err, StageRequest)
	})

	t.Run("invalid tag", func(t *testing.T) {
		directory := t.TempDir()
		writeManifest(t, directory, testModuleID, "1.0.0", testRepository, testSourcePath)

		_, err := prepare(context.Background(), Request{Directory: directory, Tag: "invalid tag"}, &fakeRegistry{}, &fakeInspector{})
		assertStage(t, err, StageRequest)
		var validationErr *validation.Errors
		if !errors.As(err, &validationErr) {
			t.Fatalf("expected specs validation error, got %v", err)
		}
	})

	t.Run("Git error", func(t *testing.T) {
		directory := t.TempDir()
		writeManifest(t, directory, testModuleID, "1.0.0", testRepository, testSourcePath)
		failure := errors.New("tag missing")

		_, err := prepare(
			context.Background(),
			Request{Directory: directory, Tag: "v1.0.0"},
			&fakeRegistry{moduleErr: registryclient.ErrModuleNotFound},
			&fakeInspector{err: failure},
		)
		if !errors.Is(err, failure) {
			t.Fatalf("expected wrapped Git failure, got %v", err)
		}
		assertStage(t, err, StageGit)
	})
}

func TestPrepareRejectsNonCanonicalModuleIdentityBeforeExternalWork(t *testing.T) {
	for _, id := range []string{
		"MONTFERRET/archive",
		"MontFerret/archive",
		"montferret/ARCHIVE",
		"montferret/Archive",
	} {
		t.Run(id, func(t *testing.T) {
			directory := t.TempDir()
			writeManifest(t, directory, id, "1.0.0", testRepository, testSourcePath)
			reader := &fakeRegistry{}
			inspector := &fakeInspector{}

			result, err := prepare(
				context.Background(),
				Request{Directory: directory, Tag: "v1.0.0"},
				reader,
				inspector,
			)
			if result != nil {
				t.Fatalf("invalid identity prepared files: %#v", result.Files)
			}
			assertStage(t, err, StageManifest)
			if reader.moduleCalls != 0 || reader.versionCalls != 0 || inspector.calls != 0 {
				t.Fatalf(
					"external work ran after identity validation failed: module=%d version=%d git=%d",
					reader.moduleCalls,
					reader.versionCalls,
					inspector.calls,
				)
			}
		})
	}
}

func TestPrepareResolvesTagWithSharedGitInspector(t *testing.T) {
	repository := t.TempDir()
	writeManifest(t, repository, testModuleID, "1.0.0", testRepository, "")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("# Archive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "init")
	runGit(t, repository, "config", "user.name", "Barn Tests")
	runGit(t, repository, "config", "user.email", "barn-tests@example.org")
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "release")
	runGit(t, repository, "tag", "v1.0.0")
	wantCommit := strings.TrimSpace(runGit(t, repository, "rev-parse", "HEAD"))

	inspector := barn.GitInspector{Resolver: func(context.Context, string) (string, error) {
		return repository, nil
	}, Analyzer: publishFixtureAnalyzer{}}
	result, err := prepare(
		context.Background(),
		Request{Directory: repository, Tag: "v1.0.0"},
		&fakeRegistry{moduleErr: registryclient.ErrModuleNotFound},
		inspector,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Version.Commit != wantCommit {
		t.Fatalf("resolved commit %q, want %q", result.Version.Commit, wantCommit)
	}
}

func TestPrepareMapsAnalysisFailuresToAPIStage(t *testing.T) {
	directory := t.TempDir()
	writeManifest(t, directory, testModuleID, "1.0.0", testRepository, "")
	inspector := &fakeInspector{err: &apiref.AnalysisError{
		Kind:     apiref.ErrorUnsupportedRegistration,
		ModuleID: testModuleID,
		Version:  "1.0.0",
		Err:      errors.New("dynamic function name"),
	}}

	result, err := prepare(
		context.Background(),
		Request{Directory: directory, Tag: "v1.0.0"},
		&fakeRegistry{moduleErr: registryclient.ErrModuleNotFound},
		inspector,
	)
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
	assertStage(t, err, StageAPI)
}

type fakeRegistry struct {
	module       *registryclient.Module
	moduleErr    error
	version      *registryclient.Version
	versionErr   error
	moduleCalls  int
	versionCalls int
}

func (registry *fakeRegistry) Module(context.Context, string) (*registryclient.Module, error) {
	registry.moduleCalls++
	return registry.module, registry.moduleErr
}

func (registry *fakeRegistry) Version(context.Context, string, string) (*registryclient.Version, error) {
	registry.versionCalls++
	return registry.version, registry.versionErr
}

type fakeInspector struct {
	release *barn.ResolvedRelease
	err     error
	calls   int
}

type publishFixtureAnalyzer struct{}

func (publishFixtureAnalyzer) Analyze(_ context.Context, _, _, _, moduleID, version string) (*registryartifact.APIReference, error) {
	return &registryartifact.APIReference{
		SchemaVersion: registryartifact.SchemaVersion,
		ID:            moduleID,
		Version:       version,
		Namespaces: []registryartifact.APINamespace{{
			Name: "ARCHIVE",
			Functions: []registryartifact.APIFunction{{
				Name:       "OPEN",
				Signatures: []registryartifact.APIFunctionSignature{{Parameters: []string{}}},
			}},
		}},
	}, nil
}

func (inspector *fakeInspector) Inspect(context.Context, registryspec.Source, string, string, string) (*barn.ResolvedRelease, error) {
	inspector.calls++

	return inspector.release, inspector.err
}

func existingRegistry(version string, source registryclient.Source) *fakeRegistry {
	return &fakeRegistry{
		module: &registryclient.Module{
			ID:       testModuleID,
			Versions: []registryclient.VersionSummary{{Version: version}},
		},
		version: &registryclient.Version{
			ID:      testModuleID,
			Version: version,
			Source:  source,
		},
	}
}

func writeManifest(t *testing.T, directory, id, version, repository, sourcePath string) {
	t.Helper()

	repositoryBlock := ""
	if repository != "" {
		repositoryBlock = fmt.Sprintf("repository:\n  url: %s\n", repository)
		if sourcePath != "" {
			repositoryBlock += fmt.Sprintf("  directory: %s\n", sourcePath)
		}
	}

	content := fmt.Sprintf(`$schema: https://schemas.ferretlang.org/module/v1.json
name: %s
namespace: ARCHIVE
version: %s
description: Archive support.
license: Apache-2.0
documentation: https://example.com/archive
%scategories:
  - files
`, id, version, repositoryBlock)

	if err := os.WriteFile(filepath.Join(directory, modulemanifest.ManifestFilename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.com/archive\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()

	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}

	return string(output)
}

func assertStage(t *testing.T, err error, stage Stage) {
	t.Helper()

	var preparationErr *Error
	if !errors.As(err, &preparationErr) || preparationErr.Stage != stage {
		t.Fatalf("expected %s-stage error, got %v", stage, err)
	}
}
