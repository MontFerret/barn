package barn

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	modulemanifest "github.com/MontFerret/specs/pkg/module"
)

func TestGitInspectorStandaloneAndMonorepository(t *testing.T) {
	for _, sourcePath := range []string{"", "modules/archive"} {
		t.Run(strings.ReplaceAll(sourcePath, "/", "_"), func(t *testing.T) {
			manifest := testModuleManifest("montferret/archive", "ARCHIVE", "1.2.0", "Work with archives.")
			manifest.Compatibility = &modulemanifest.Compatibility{Ferret: ">=2.1.0"}
			manifest.Repository.Directory = sourcePath
			fixture := newGitFixture(t, sourcePath, manifest, "archive/v1.2.0", true)
			registryManifest := testRegistryManifest("montferret", "archive")
			registryManifest.Source.Path = sourcePath
			registry := &Registry{Modules: []*Module{{
				Manifest: registryManifest,
				Versions: []*Version{{Record: testVersion("1.2.0", "archive/v1.2.0", fixture.commit)}},
			}}}

			inspector := fixtureGitInspector(fixture.directory)
			if err := inspector.Resolve(context.Background(), registry); err != nil {
				t.Fatal(err)
			}

			got := registry.Modules[0].Versions[0].Manifest
			if got == nil || got.Namespace != "ARCHIVE" {
				t.Fatalf("manifest not resolved: %#v", got)
			}

			if got.Repository == nil || got.Repository.URL != "https://fixtures.invalid/source.git" || got.Repository.Directory != sourcePath {
				t.Fatalf("repository metadata not decoded: %#v", got.Repository)
			}

			if got := string(registry.Modules[0].Versions[0].Documentation); got != testDocumentation {
				t.Fatalf("documentation not resolved: %q", got)
			}

			if got := registry.Modules[0].Versions[0].PackagePath; got != testPackagePath {
				t.Fatalf("package path = %q, want %q", got, testPackagePath)
			}
		})
	}
}

func TestGitInspectorValidatesPinnedGoModule(t *testing.T) {
	for _, test := range []struct {
		name        string
		version     string
		sourcePath  string
		goMod       []byte
		wantPackage string
		wantError   string
	}{
		{
			name:        "vanity path ignores repository and source directory",
			version:     "1.2.0",
			sourcePath:  "modules/archive",
			goMod:       []byte("module modules.example.org/tools/archive\n\ngo 1.25.0\n"),
			wantPackage: "modules.example.org/tools/archive",
		},
		{
			name:        "major version suffix",
			version:     "2.1.0",
			goMod:       []byte("module modules.example.org/tools/archive/v2\n\ngo 1.25.0\n"),
			wantPackage: "modules.example.org/tools/archive/v2",
		},
		{
			name:      "missing go.mod",
			version:   "1.2.0",
			goMod:     nil,
			wantError: modulePackageFilename,
		},
		{
			name:      "malformed go.mod",
			version:   "1.2.0",
			goMod:     []byte("module [invalid\n"),
			wantError: "validate go.mod",
		},
		{
			name:      "missing module directive",
			version:   "1.2.0",
			goMod:     []byte("go 1.25.0\n"),
			wantError: "module directive is required",
		},
		{
			name:      "invalid module path",
			version:   "1.2.0",
			goMod:     []byte("module archive\n\ngo 1.25.0\n"),
			wantError: "malformed module path",
		},
		{
			name:      "major version mismatch",
			version:   "2.1.0",
			goMod:     []byte("module modules.example.org/tools/archive\n\ngo 1.25.0\n"),
			wantError: "incompatible with version",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest := testModuleManifest("montferret/archive", "ARCHIVE", test.version, "Archives.")
			manifest.Repository.Directory = test.sourcePath
			fixture := newGitFixtureRepository(
				t,
				test.sourcePath,
				manifest,
				modulemanifest.ManifestFilename,
				"v"+test.version,
				false,
				[]byte(testDocumentation),
				test.goMod,
			)
			registry := registryForFixture(fixture, test.version, "v"+test.version)
			registry.Modules[0].Manifest.Source.Path = test.sourcePath

			err := fixtureGitInspector(fixture.directory).Resolve(context.Background(), registry)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("expected error containing %q, got %v", test.wantError, err)
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}
			if got := registry.Modules[0].Versions[0].PackagePath; got != test.wantPackage {
				t.Fatalf("package path = %q, want %q", got, test.wantPackage)
			}
		})
	}
}

func TestGitInspectorRejectsInvalidCategoryID(t *testing.T) {
	manifest := testModuleManifest("montferret/archive", "ARCHIVE", "1.0.0", "Archives.")
	manifest.Categories = []string{"../legacy"}
	fixture := newGitFixture(t, "", manifest, "v1.0.0", false)
	registry := registryForFixture(fixture, "1.0.0", "v1.0.0")

	err := fixtureGitInspector(fixture.directory).Resolve(context.Background(), registry)
	if err == nil || !strings.Contains(err.Error(), "montferret/archive@1.0.0") || !strings.Contains(err.Error(), `category "../legacy"`) || !strings.Contains(err.Error(), categoryIDPatternText) {
		t.Fatalf("expected contextual category validation error, got %v", err)
	}
}

func TestGitInspectorRequiresPinnedDocumentation(t *testing.T) {
	manifest := testModuleManifest("montferret/archive", "ARCHIVE", "1.0.0", "Archives.")
	fixture := newGitFixtureWithoutDocumentation(t, "", manifest, "v1.0.0", false)
	registry := registryForFixture(fixture, "1.0.0", "v1.0.0")

	err := fixtureGitInspector(fixture.directory).Resolve(context.Background(), registry)
	if err == nil || !strings.Contains(err.Error(), moduleDocumentationFilename) {
		t.Fatalf("expected missing %s error, got %v", moduleDocumentationFilename, err)
	}
}

func TestGitInspectorDoesNotDiscoverLegacyManifestFilenames(t *testing.T) {
	for _, filename := range []string{"ferret-module.yaml", "ferret.module.yaml", "module.yaml"} {
		t.Run(filename, func(t *testing.T) {
			manifest := testModuleManifest("montferret/archive", "ARCHIVE", "1.0.0", "Archives.")
			fixture := newGitFixtureWithManifestFilename(t, "", manifest, filename, "v1.0.0", false)
			registry := registryForFixture(fixture, "1.0.0", "v1.0.0")

			err := fixtureGitInspector(fixture.directory).Resolve(context.Background(), registry)
			if err == nil || !strings.Contains(err.Error(), modulemanifest.ManifestFilename) {
				t.Fatalf("expected missing %s error, got %v", modulemanifest.ManifestFilename, err)
			}
		})
	}
}

func TestGitInspectorLightweightTag(t *testing.T) {
	manifest := testModuleManifest("montferret/archive", "ARCHIVE", "1.0.0", "Archives.")
	fixture := newGitFixture(t, "", manifest, "v1.0.0", false)
	registry := registryForFixture(fixture, "1.0.0", "v1.0.0")
	if err := fixtureGitInspector(fixture.directory).Resolve(context.Background(), registry); err != nil {
		t.Fatal(err)
	}
}

func TestGitInspectorMaterializesAndAnalyzesPinnedCommit(t *testing.T) {
	const sourcePath = "modules/archive"
	manifest := testModuleManifest("montferret/archive", "ARCHIVE", "1.0.0", "Archives.")
	manifest.Repository.Directory = sourcePath
	fixture := newGitFixture(t, sourcePath, manifest, "", false)
	writeAnalyzableGitFixture(t, fixture.directory, sourcePath)
	runTestGit(t, fixture.directory, "add", ".")
	runTestGit(t, fixture.directory, "commit", "-m", "add analyzable source")
	fixture.commit = strings.TrimSpace(runTestGit(t, fixture.directory, "rev-parse", "HEAD"))
	runTestGit(t, fixture.directory, "tag", "v1.0.0")

	// A dirty working tree must not affect analysis of the pinned commit.
	if err := os.WriteFile(filepath.Join(fixture.directory, sourcePath, "module.go"), []byte("package archive\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := registryForFixture(fixture, "1.0.0", "v1.0.0")
	registry.Modules[0].Manifest.Source.Path = sourcePath
	inspector := GitInspector{Resolver: fixtureResolver(fixture.directory)}
	if err := inspector.Resolve(context.Background(), registry); err != nil {
		t.Fatalf("resolve real API analysis fixture: %v", err)
	}

	api := registry.Modules[0].Versions[0].API
	if api == nil || len(api.Namespaces) != 1 || api.Namespaces[0].Name != "FIXTURE" {
		t.Fatalf("API Reference = %#v", api)
	}
	if got, want := api.Namespaces[0].Functions[0].Name, "RUN"; got != want {
		t.Fatalf("function name = %q, want %q", got, want)
	}
}

func TestGitInspectorFailures(t *testing.T) {
	validManifest := testModuleManifest("montferret/archive", "ARCHIVE", "1.0.0", "Archives.")
	for name, setup := range map[string]func(*testing.T) (*Registry, RepositoryResolver){
		"inaccessible repository": func(t *testing.T) (*Registry, RepositoryResolver) {
			registry := registryForFixture(gitFixture{commit: testCommit}, "1.0.0", "v1.0.0")
			return registry, fixtureResolver(t.TempDir() + "/missing.git")
		},
		"missing source path": func(t *testing.T) (*Registry, RepositoryResolver) {
			fixture := newGitFixture(t, "", validManifest, "v1.0.0", false)
			registry := registryForFixture(fixture, "1.0.0", "v1.0.0")
			registry.Modules[0].Manifest.Source.Path = "missing"
			return registry, fixtureResolver(fixture.directory)
		},
		"missing tag": func(t *testing.T) (*Registry, RepositoryResolver) {
			fixture := newGitFixture(t, "", validManifest, "v1.0.0", false)
			return registryForFixture(fixture, "1.0.0", "v2.0.0"), fixtureResolver(fixture.directory)
		},
		"tag commit mismatch": func(t *testing.T) (*Registry, RepositoryResolver) {
			fixture := newGitFixture(t, "", validManifest, "v1.0.0", false)
			registry := registryForFixture(fixture, "1.0.0", "v1.0.0")
			registry.Modules[0].Versions[0].Record.Commit = testCommit
			return registry, fixtureResolver(fixture.directory)
		},
		"missing source manifest": func(t *testing.T) (*Registry, RepositoryResolver) {
			fixture := newGitFixture(t, "", nil, "v1.0.0", false)
			return registryForFixture(fixture, "1.0.0", "v1.0.0"), fixtureResolver(fixture.directory)
		},
		"invalid source manifest": func(t *testing.T) (*Registry, RepositoryResolver) {
			manifest := testModuleManifest("montferret/archive", "INVALID-NAMESPACE", "1.0.0", "Archives.")
			fixture := newGitFixture(t, "", manifest, "v1.0.0", false)
			return registryForFixture(fixture, "1.0.0", "v1.0.0"), fixtureResolver(fixture.directory)
		},
		"name mismatch": func(t *testing.T) (*Registry, RepositoryResolver) {
			manifest := testModuleManifest("acme/archive", "ARCHIVE", "1.0.0", "Archives.")
			fixture := newGitFixture(t, "", manifest, "v1.0.0", false)
			return registryForFixture(fixture, "1.0.0", "v1.0.0"), fixtureResolver(fixture.directory)
		},
		"version mismatch": func(t *testing.T) (*Registry, RepositoryResolver) {
			manifest := testModuleManifest("montferret/archive", "ARCHIVE", "2.0.0", "Archives.")
			fixture := newGitFixture(t, "", manifest, "v1.0.0", false)
			return registryForFixture(fixture, "1.0.0", "v1.0.0"), fixtureResolver(fixture.directory)
		},
		"invalid compatibility": func(t *testing.T) (*Registry, RepositoryResolver) {
			manifest := testModuleManifest("montferret/archive", "ARCHIVE", "1.0.0", "Archives.")
			manifest.Compatibility = &modulemanifest.Compatibility{Ferret: "definitely not a range"}
			fixture := newGitFixture(t, "", manifest, "v1.0.0", false)
			return registryForFixture(fixture, "1.0.0", "v1.0.0"), fixtureResolver(fixture.directory)
		},
	} {
		t.Run(name, func(t *testing.T) {
			registry, resolver := setup(t)
			if err := (GitInspector{Resolver: resolver, Analyzer: fixtureAPIAnalyzer{}}).Resolve(context.Background(), registry); err == nil {
				t.Fatal("expected inspection to fail")
			}
		})
	}
}

func TestPublicAddressClassification(t *testing.T) {
	for _, repository := range []string{
		"https://localhost/repository.git",
		"https://127.0.0.1/repository.git",
		"https://[::1]/repository.git",
	} {
		if err := requirePublicRepository(context.Background(), repository); err == nil {
			t.Errorf("expected %s to be rejected", repository)
		}
	}
}

func TestNonPublicAddressRanges(t *testing.T) {
	for _, address := range []string{"10.0.0.1", "100.64.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "2001:db8::1"} {
		if isPublicAddress(netip.MustParseAddr(address)) {
			t.Errorf("expected %s to be non-public", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !isPublicAddress(netip.MustParseAddr(address)) {
			t.Errorf("expected %s to be public", address)
		}
	}
}

func TestCleanGitEnvironment(t *testing.T) {
	cleaned := cleanGitEnvironment([]string{
		"PATH=/usr/bin",
		"HOME=/private/home",
		"GIT_TOKEN=secret",
		"HTTPS_PROXY=https://proxy.example",
		"no_proxy=localhost",
	})
	if len(cleaned) != 1 || cleaned[0] != "PATH=/usr/bin" {
		t.Fatalf("unsafe environment survived: %#v", cleaned)
	}
}

func registryForFixture(fixture gitFixture, version, tag string) *Registry {
	return &Registry{Modules: []*Module{{
		Manifest: testRegistryManifest("montferret", "archive"),
		Versions: []*Version{{Record: testVersion(version, tag, fixture.commit)}},
	}}}
}

func writeAnalyzableGitFixture(t *testing.T, repository, sourcePath string) {
	t.Helper()
	files := map[string]string{
		filepath.Join(sourcePath, "go.mod"): `module example.com/archive

go 1.25.0

require github.com/MontFerret/ferret/v2 v2.0.0

replace github.com/MontFerret/ferret/v2 => ../../ferret
`,
		filepath.Join(sourcePath, "module.go"): `package archive

import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)

func New() module.Module {
	return sdk.NewModule("archive", func(bootstrap module.Bootstrap) error {
		return sdk.RegisterFunctions(
			bootstrap.Host().Library().Namespace("FIXTURE"),
			sdk.Func("RUN", Run),
		)
	})
}

// Run executes the fixture function.
func Run(context.Context) (runtime.Value, error) { return nil, nil }
`,
		"ferret/go.mod": "module github.com/MontFerret/ferret/v2\n\ngo 1.25.0\n",
		"ferret/pkg/runtime/runtime.go": `package runtime

import "context"

type Value = any
type Function0 = func(context.Context) (Value, error)
type FunctionDef interface { A0() FunctionCollection[Function0] }
type FunctionCollection[T any] interface { Add(string, T) FunctionCollection[T] }
type Namespace interface { Namespace(string) Namespace; Function() FunctionDef }
type Library interface { Namespace; Build() error }
`,
		"ferret/pkg/module/module.go": `package module

import "github.com/MontFerret/ferret/v2/pkg/runtime"

type Module interface { Register(Bootstrap) error }
type HostContext interface { Library() runtime.Library }
type Bootstrap interface { Host() HostContext }
`,
		"ferret/pkg/sdk/sdk.go": `package sdk

import (
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
)

type FunctionDef struct{}
func NewModule(string, func(module.Bootstrap) error) module.Module { return nil }
func Func(string, runtime.Function0) FunctionDef { return FunctionDef{} }
func RegisterFunctions(runtime.Namespace, ...FunctionDef) error { return nil }
`,
	}
	for name, contents := range files {
		filename := filepath.Join(repository, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
