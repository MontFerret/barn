package barn

import (
	"context"
	"net/netip"
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

			inspector := GitInspector{Resolver: fixtureResolver(fixture.directory)}
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
		})
	}
}

func TestGitInspectorRejectsInvalidCategoryID(t *testing.T) {
	manifest := testModuleManifest("montferret/archive", "ARCHIVE", "1.0.0", "Archives.")
	manifest.Categories = []string{"../legacy"}
	fixture := newGitFixture(t, "", manifest, "v1.0.0", false)
	registry := registryForFixture(fixture, "1.0.0", "v1.0.0")

	err := (GitInspector{Resolver: fixtureResolver(fixture.directory)}).Resolve(context.Background(), registry)
	if err == nil || !strings.Contains(err.Error(), "montferret/archive@1.0.0") || !strings.Contains(err.Error(), `category "../legacy"`) || !strings.Contains(err.Error(), categoryIDPatternText) {
		t.Fatalf("expected contextual category validation error, got %v", err)
	}
}

func TestGitInspectorRequiresPinnedDocumentation(t *testing.T) {
	manifest := testModuleManifest("montferret/archive", "ARCHIVE", "1.0.0", "Archives.")
	fixture := newGitFixtureWithoutDocumentation(t, "", manifest, "v1.0.0", false)
	registry := registryForFixture(fixture, "1.0.0", "v1.0.0")

	err := (GitInspector{Resolver: fixtureResolver(fixture.directory)}).Resolve(context.Background(), registry)
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

			err := (GitInspector{Resolver: fixtureResolver(fixture.directory)}).Resolve(context.Background(), registry)
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
	if err := (GitInspector{Resolver: fixtureResolver(fixture.directory)}).Resolve(context.Background(), registry); err != nil {
		t.Fatal(err)
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
			if err := (GitInspector{Resolver: resolver}).Resolve(context.Background(), registry); err == nil {
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
