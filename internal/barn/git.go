package barn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MontFerret/barn/internal/barn/apiref"
	"github.com/MontFerret/specs/pkg/api"
	modulemanifest "github.com/MontFerret/specs/pkg/module"
	registryspec "github.com/MontFerret/specs/pkg/registry"
)

type (
	// Inspector resolves pinned module manifests from registry Git sources.
	Inspector interface {
		Resolve(context.Context, *Registry) error
	}

	// RepositoryResolver maps a validated registry URL to the URL used by Git.
	// Production leaves this nil; tests use it to route fixture HTTPS URLs locally.
	RepositoryResolver func(context.Context, string) (string, error)

	// APIAnalyzer derives a complete API Reference from one materialized release.
	APIAnalyzer interface {
		Analyze(context.Context, string, string, string, string, string) (*api.Reference, error)
	}

	// GitInspector inspects release refs and blobs using the Git executable.
	GitInspector struct {
		Resolver RepositoryResolver
		Analyzer APIAnalyzer
		Timeout  time.Duration
	}

	// ResolvedRelease contains the authoritative content at one resolved Git tag.
	ResolvedRelease struct {
		Commit        string
		Manifest      *modulemanifest.Manifest
		PackagePath   string
		Documentation []byte
		API           *api.Reference
	}

	sourceRepository struct {
		directory string
		local     bool
	}
)

const moduleDocumentationFilename = "README.md"

// Resolve validates remote release identities and attaches authoritative manifests.
func (inspector GitInspector) Resolve(ctx context.Context, registry *Registry) error {
	temporaryRoot, err := os.MkdirTemp("", "barn-git-")
	if err != nil {
		return fmt.Errorf("create temporary Git root: %w", err)
	}

	defer os.RemoveAll(temporaryRoot)

	repositories := make(map[string]sourceRepository)

	for _, registryModule := range registry.Modules {
		source := registryModule.Manifest.Source
		repository, exists := repositories[source.Repository]

		if !exists {
			resolvedURL, local, err := inspector.resolveRepository(ctx, source.Repository)
			if err != nil {
				return fmt.Errorf("resolve repository for %s: %w", registryModule.ID(), err)
			}

			directory := filepath.Join(temporaryRoot, fmt.Sprintf("repository-%d.git", len(repositories)))
			if _, err := runGit(ctx, "", local, temporaryRoot, "init", "--bare", directory); err != nil {
				return err
			}

			if _, err := runGit(ctx, directory, local, temporaryRoot, "remote", "add", "origin", resolvedURL); err != nil {
				return err
			}

			repository = sourceRepository{directory: directory, local: local}
			repositories[source.Repository] = repository
		}

		for _, version := range registryModule.Versions {
			operationContext, cancel := context.WithTimeout(ctx, inspector.timeout())
			release, err := inspectRelease(
				operationContext,
				repository.directory,
				repository.local,
				temporaryRoot,
				inspector.analyzer(),
				registryModule.Manifest.Source,
				registryModule.ID(),
				version.Record.Version,
				version.Record.Tag,
				version.Record.Commit,
			)

			cancel()

			if err != nil {
				return err
			}

			version.Manifest = release.Manifest
			version.PackagePath = release.PackagePath
			version.Documentation = append([]byte{}, release.Documentation...)
			version.API = release.API
		}
	}

	return nil
}

// Inspect resolves one release tag and returns its authoritative pinned content.
func (inspector GitInspector) Inspect(ctx context.Context, source registryspec.Source, moduleID, version, tag string) (*ResolvedRelease, error) {
	temporaryRoot, err := os.MkdirTemp("", "barn-git-")
	if err != nil {
		return nil, fmt.Errorf("create temporary Git root: %w", err)
	}
	defer os.RemoveAll(temporaryRoot)

	resolvedURL, local, err := inspector.resolveRepository(ctx, source.Repository)
	if err != nil {
		return nil, fmt.Errorf("resolve repository for %s: %w", moduleID, err)
	}

	directory := filepath.Join(temporaryRoot, "repository.git")
	if _, err := runGit(ctx, "", local, temporaryRoot, "init", "--bare", directory); err != nil {
		return nil, err
	}

	if _, err := runGit(ctx, directory, local, temporaryRoot, "remote", "add", "origin", resolvedURL); err != nil {
		return nil, err
	}

	operationContext, cancel := context.WithTimeout(ctx, inspector.timeout())
	defer cancel()

	return inspectRelease(operationContext, directory, local, temporaryRoot, inspector.analyzer(), source, moduleID, version, tag, "")
}

func (inspector GitInspector) analyzer() APIAnalyzer {
	if inspector.Analyzer != nil {
		return inspector.Analyzer
	}

	return apiref.Analyzer{}
}

func (inspector GitInspector) timeout() time.Duration {
	if inspector.Timeout == 0 {
		return 2 * time.Minute
	}

	return inspector.Timeout
}

func (inspector GitInspector) resolveRepository(ctx context.Context, repository string) (string, bool, error) {
	if inspector.Resolver != nil {
		resolved, err := inspector.Resolver(ctx, repository)

		return resolved, true, err
	}

	if err := requirePublicRepository(ctx, repository); err != nil {
		return "", false, err
	}

	return repository, false, nil
}
