package barn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	registryartifact "github.com/MontFerret/specs/pkg/registry/artifact"
)

type (
	// GenerationMode controls whether generation reuses published release artifacts.
	GenerationMode string

	// GenerationOptions configures a complete distribution build.
	GenerationOptions struct {
		Root         string
		Previous     string
		SourceCommit string
		Mode         GenerationMode
		Inspector    Inspector
	}

	// GenerationResult contains a validated candidate distribution.
	GenerationResult struct {
		Distribution *Distribution
		SourceCommit string
		Mode         GenerationMode
	}
)

const (
	GenerationModeFull        GenerationMode = "full"
	GenerationModeAuto        GenerationMode = "auto"
	GenerationModeIncremental GenerationMode = "incremental"
)

// BuildDistribution enriches the required releases and returns a complete validated candidate tree.
func BuildDistribution(ctx context.Context, options GenerationOptions) (*GenerationResult, error) {
	root := options.Root
	if root == "" {
		root = "."
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve Barn repository root: %w", err)
	}

	mode := options.Mode
	if mode == "" {
		mode = GenerationModeFull
	}

	if mode != GenerationModeFull && mode != GenerationModeAuto && mode != GenerationModeIncremental {
		return nil, fmt.Errorf("unsupported generation mode %q", mode)
	}

	sourceRevision := options.SourceCommit
	if sourceRevision == "" {
		sourceRevision = "HEAD"
	}

	sourceCommit, err := resolveRepositoryCommit(ctx, root, sourceRevision)
	if err != nil {
		return nil, err
	}

	registry, err := Load(root)
	if err != nil {
		return nil, err
	}

	inspector := options.Inspector
	if inspector == nil {
		inspector = GitInspector{}
	}

	if mode == GenerationModeFull {
		return buildFullDistribution(ctx, registry, inspector, sourceCommit)
	}

	if options.Previous == "" {
		if mode == GenerationModeAuto {
			return buildFullDistribution(ctx, registry, inspector, sourceCommit)
		}

		return nil, fmt.Errorf("incremental generation requires --previous")
	}

	previousPath, err := filepath.Abs(options.Previous)
	if err != nil {
		return nil, fmt.Errorf("resolve previous distribution path: %w", err)
	}

	if _, err := os.Lstat(previousPath); err != nil {
		if os.IsNotExist(err) && mode == GenerationModeAuto {
			return buildFullDistribution(ctx, registry, inspector, sourceCommit)
		}

		return nil, fmt.Errorf("inspect previous distribution: %w", err)
	}

	cached, err := readCachedDistribution(previousPath)
	if err != nil {
		if errors.Is(err, errLegacyDistribution) && mode == GenerationModeAuto {
			return buildFullDistribution(ctx, registry, inspector, sourceCommit)
		}

		return nil, err
	}

	if err := requireAncestorCommit(ctx, root, cached.sourceCommit, sourceCommit); err != nil {
		return nil, err
	}

	changedPaths, err := changedRepositoryPaths(ctx, root, cached.sourceCommit, sourceCommit)
	if err != nil {
		return nil, err
	}

	registryPaths := make([]string, 0, len(changedPaths))
	nonRegistryPath := ""
	for _, changedPath := range changedPaths {
		if changedPath != registrySourcePath && !strings.HasPrefix(changedPath, registrySourcePath+"/") {
			if nonRegistryPath == "" {
				nonRegistryPath = changedPath
			}

			continue
		}

		registryPaths = append(registryPaths, changedPath)
	}

	if nonRegistryPath != "" {
		if mode == GenerationModeAuto {
			return buildFullDistribution(ctx, registry, inspector, sourceCommit)
		}

		return nil, fmt.Errorf("incremental generation cannot reuse artifacts after non-Registry change %s", nonRegistryPath)
	}

	affected, err := affectedReleases(registry, registryPaths)
	if err != nil {
		return nil, err
	}

	if err := hydrateRegistryFromDistribution(registry, cached, affected); err != nil {
		return nil, err
	}

	selected := filterRegistryReleases(registry, affected)
	if len(selected.Modules) != 0 {
		if err := inspector.Resolve(ctx, selected); err != nil {
			return nil, err
		}
	}

	distribution, err := GenerateDistribution(registry, sourceCommit)
	if err != nil {
		return nil, err
	}

	if err := validateGeneratedDistribution(root, distribution, sourceCommit); err != nil {
		return nil, err
	}

	return &GenerationResult{Distribution: distribution, SourceCommit: sourceCommit, Mode: GenerationModeIncremental}, nil
}

// ValidateDistributionTree validates an on-disk candidate against canonical source without remote enrichment.
func ValidateDistributionTree(ctx context.Context, root, directory, sourceRevision string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve Barn repository root: %w", err)
	}

	if sourceRevision == "" {
		sourceRevision = "HEAD"
	}

	sourceCommit, err := resolveRepositoryCommit(ctx, root, sourceRevision)
	if err != nil {
		return err
	}

	distribution, err := readDistributionPath(directory)
	if err != nil {
		return err
	}

	return validateGeneratedDistribution(root, distribution, sourceCommit)
}

func buildFullDistribution(ctx context.Context, registry *Registry, inspector Inspector, sourceCommit string) (*GenerationResult, error) {
	if err := inspector.Resolve(ctx, registry); err != nil {
		return nil, err
	}

	distribution, err := GenerateDistribution(registry, sourceCommit)
	if err != nil {
		return nil, err
	}

	if err := validateGeneratedDistribution(registry.Root, distribution, sourceCommit); err != nil {
		return nil, err
	}

	return &GenerationResult{Distribution: distribution, SourceCommit: sourceCommit, Mode: GenerationModeFull}, nil
}

func validateGeneratedDistribution(root string, distribution *Distribution, sourceCommit string) error {
	rootData, exists := distribution.Files["index.json"]
	if !exists {
		return fmt.Errorf("generated index.json is missing")
	}

	rootIndex, err := registryartifact.ParseRootIndex(rootData)
	if err != nil {
		return fmt.Errorf("parse generated index.json: %w", err)
	}

	if rootIndex.Source.Commit != sourceCommit {
		return fmt.Errorf("generated source commit is %s, want %s", rootIndex.Source.Commit, sourceCommit)
	}

	registry, err := Load(root)
	if err != nil {
		return err
	}

	cached := &cachedDistribution{distribution: distribution, sourceCommit: sourceCommit}
	if err := hydrateRegistryFromDistribution(registry, cached, map[string]struct{}{}); err != nil {
		return fmt.Errorf("validate generated distribution: %w", err)
	}

	return nil
}

func resolveRepositoryCommit(ctx context.Context, root, revision string) (string, error) {
	data, err := runRepositoryGit(ctx, root, "rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve source commit %q: %w", revision, err)
	}

	return strings.TrimSpace(string(data)), nil
}

func requireAncestorCommit(ctx context.Context, root, ancestor, descendant string) error {
	if _, err := runRepositoryGit(ctx, root, "merge-base", "--is-ancestor", ancestor, descendant); err != nil {
		return fmt.Errorf("published source commit %s is not an ancestor of %s: %w", ancestor, descendant, err)
	}

	return nil
}

func changedRepositoryPaths(ctx context.Context, root, base, target string) ([]string, error) {
	data, err := runRepositoryGit(ctx, root, "diff", "--name-only", "--diff-filter=ACDMRTUXB", base, target, "--")
	if err != nil {
		return nil, fmt.Errorf("list changes from %s to %s: %w", base, target, err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, nil
	}

	return lines, nil
}

func affectedReleases(registry *Registry, changedPaths []string) (map[string]struct{}, error) {
	modules := make(map[string]*Module, len(registry.Modules))
	for _, module := range registry.Modules {
		modules[module.ID()] = module
	}

	affected := make(map[string]struct{})
	for _, changedPath := range changedPaths {
		parts := strings.Split(filepath.ToSlash(changedPath), "/")
		if len(parts) == 5 && strings.Join(parts[:2], "/") == moduleRegistryPath && parts[4] == "manifest.json" {
			moduleID := parts[2] + "/" + parts[3]
			module, exists := modules[moduleID]
			if !exists {
				return nil, fmt.Errorf("published module manifest %s was deleted", changedPath)
			}

			for _, version := range module.Versions {
				affected[releaseKey(moduleID, version.Record.Version)] = struct{}{}
			}

			continue
		}

		if len(parts) == 6 && strings.Join(parts[:2], "/") == moduleRegistryPath && parts[4] == "versions" {
			match := versionFilename.FindStringSubmatch(parts[5])
			if match == nil {
				return nil, fmt.Errorf("changed Registry path %s is not canonical", changedPath)
			}

			moduleID := parts[2] + "/" + parts[3]
			module, exists := modules[moduleID]
			if !exists || findVersion(module, match[1]) == nil {
				return nil, fmt.Errorf("published version record %s was deleted", changedPath)
			}

			affected[releaseKey(moduleID, match[1])] = struct{}{}

			continue
		}

		if changedPath == pluginRegistryPath+"/.gitkeep" || changedPath == moduleRegistryPath+"/.gitkeep" {
			continue
		}

		return nil, fmt.Errorf("changed Registry path %s is not canonical", changedPath)
	}

	return affected, nil
}

func filterRegistryReleases(registry *Registry, affected map[string]struct{}) *Registry {
	filtered := &Registry{Root: registry.Root, Modules: make([]*Module, 0)}
	for _, module := range registry.Modules {
		versions := make([]*Version, 0)
		for _, version := range module.Versions {
			if _, selected := affected[releaseKey(module.ID(), version.Record.Version)]; selected {
				versions = append(versions, version)
			}
		}

		if len(versions) != 0 {
			filtered.Modules = append(filtered.Modules, &Module{
				Directory: module.Directory,
				Manifest:  module.Manifest,
				Versions:  versions,
			})
		}
	}

	return filtered
}

func findVersion(module *Module, version string) *Version {
	for _, candidate := range module.Versions {
		if candidate.Record.Version == version {
			return candidate
		}
	}

	return nil
}
