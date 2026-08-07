package barn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/MontFerret/barn/internal/registrydist"
)

const (
	distributionPath      = "dist"
	categoryIDPatternText = `^[a-z0-9]+(?:-[a-z0-9]+)*$`
)

var categoryIDPattern = regexp.MustCompile(categoryIDPatternText)

type (
	// Distribution is the complete generated public registry representation.
	// File paths are slash-separated and relative to dist/.
	Distribution struct {
		Files map[string][]byte
	}

	RootIndex             = registrydist.RootIndex
	ModuleIndex           = registrydist.ModuleIndex
	ModuleIndexEntry      = registrydist.ModuleIndexEntry
	CategoryIndex         = registrydist.CategoryIndex
	CategoryIndexEntry    = registrydist.CategoryIndexEntry
	CategoryDocument      = registrydist.CategoryDocument
	CategorySummary       = registrydist.CategorySummary
	PluginIndex           = registrydist.PluginIndex
	ModuleDocument        = registrydist.ModuleDocument
	ModuleDocumentVersion = registrydist.ModuleDocumentVersion
	VersionDocument       = registrydist.VersionDocument
	VersionPackage        = registrydist.VersionPackage
	VersionSource         = registrydist.VersionSource

	moduleProjection struct {
		indexEntry  ModuleIndexEntry
		categoryIDs []string
	}

	categoryAccumulator struct {
		summary CategorySummary
		modules []ModuleIndexEntry
	}
)

// GenerateDistribution projects a resolved Registry into the complete public dist/ tree.
func GenerateDistribution(registry *Registry) (*Distribution, error) {
	if registry == nil {
		return nil, fmt.Errorf("registry is nil")
	}

	distribution := &Distribution{Files: make(map[string][]byte)}
	if err := distribution.addJSON("index.json", RootIndex{
		SchemaVersion: registrydist.SchemaVersion,
		Artifacts: map[string]string{
			"categories": "/categories.json",
			"modules":    "/modules/index.json",
			"plugins":    "/plugins/index.json",
		},
	}); err != nil {
		return nil, err
	}

	if err := distribution.addJSON("plugins/index.json", PluginIndex{
		SchemaVersion: registrydist.SchemaVersion,
		Plugins:       make([]json.RawMessage, 0),
	}); err != nil {
		return nil, err
	}

	modules := append([]*Module(nil), registry.Modules...)
	sort.Slice(modules, func(i, j int) bool { return modules[i].ID() < modules[j].ID() })
	index := ModuleIndex{SchemaVersion: registrydist.SchemaVersion, Modules: make([]ModuleIndexEntry, 0, len(modules))}
	categories := make(map[string]*categoryAccumulator)

	for _, registryModule := range modules {
		projection, err := addModuleToDistribution(distribution, registryModule)
		if err != nil {
			return nil, err
		}

		index.Modules = append(index.Modules, projection.indexEntry)
		for _, categoryID := range projection.categoryIDs {
			category, exists := categories[categoryID]
			if !exists {
				category = &categoryAccumulator{summary: CategorySummary{
					ID:   categoryID,
					Name: categoryDisplayName(categoryID),
				}}
				categories[categoryID] = category
			}

			category.modules = append(category.modules, projection.indexEntry)
		}
	}

	if err := distribution.addJSON("modules/index.json", index); err != nil {
		return nil, err
	}

	categoryIndex := CategoryIndex{
		SchemaVersion: registrydist.SchemaVersion,
		Categories:    make([]CategoryIndexEntry, 0, len(categories)),
	}

	for _, categoryID := range sortedDistributionPaths(categories) {
		category := categories[categoryID]

		sort.Slice(category.modules, func(i, j int) bool {
			return category.modules[i].ID < category.modules[j].ID
		})

		categoryPath := path.Join("categories", categoryID+".json")
		categoryIndex.Categories = append(categoryIndex.Categories, CategoryIndexEntry{
			ID:    category.summary.ID,
			Name:  category.summary.Name,
			Count: len(category.modules),
			Href:  "/" + categoryPath,
		})

		if err := distribution.addJSON(categoryPath, CategoryDocument{
			SchemaVersion: registrydist.SchemaVersion,
			Category:      category.summary,
			Modules:       category.modules,
		}); err != nil {
			return nil, err
		}
	}

	if err := distribution.addJSON("categories.json", categoryIndex); err != nil {
		return nil, err
	}

	return distribution, nil
}

func addModuleToDistribution(distribution *Distribution, registryModule *Module) (moduleProjection, error) {
	versions := append([]*Version(nil), registryModule.Versions...)
	if len(versions) == 0 {
		return moduleProjection{}, fmt.Errorf("module %s has no versions", registryModule.ID())
	}

	if err := sortVersions(versions); err != nil {
		return moduleProjection{}, fmt.Errorf("sort versions for %s: %w", registryModule.ID(), err)
	}

	metadataVersion := versions[0]
	latest := ""

	for _, version := range versions {
		if version.Manifest == nil {
			return moduleProjection{}, fmt.Errorf("module %s@%s has not been resolved", registryModule.ID(), version.Record.Version)
		}

		if version.Documentation == nil {
			return moduleProjection{}, fmt.Errorf("module %s@%s documentation has not been resolved", registryModule.ID(), version.Record.Version)
		}

		if version.PackagePath == "" {
			return moduleProjection{}, fmt.Errorf("module %s@%s package has not been resolved", registryModule.ID(), version.Record.Version)
		}

		if version.Record.PublishedAt == nil {
			return moduleProjection{}, fmt.Errorf("module %s@%s has not been publication-stamped", registryModule.ID(), version.Record.Version)
		}

		if err := validateCategoryIDs(registryModule.ID(), version.Record.Version, version.Manifest.Categories); err != nil {
			return moduleProjection{}, err
		}

		parsed, _ := semver.StrictNewVersion(version.Record.Version)
		if latest == "" && parsed.Prerelease() == "" {
			latest = version.Record.Version
			metadataVersion = version
		}
	}

	modulePath := path.Join("modules", registryModule.Manifest.Owner, registryModule.Manifest.Name)
	indexEntry := ModuleIndexEntry{
		ID:     registryModule.ID(),
		Latest: latest,
		Href:   "/" + path.Join(modulePath, "index.json"),
	}

	document := ModuleDocument{
		SchemaVersion: registrydist.SchemaVersion,
		ID:            registryModule.ID(),
		Owner:         registryModule.Manifest.Owner,
		Name:          registryModule.Manifest.Name,
		Description:   metadataVersion.Manifest.Description,
		Latest:        latest,
		Versions:      make([]ModuleDocumentVersion, 0, len(versions)),
	}

	for _, version := range versions {
		versionPath := path.Join(modulePath, "versions", version.Record.Version)
		document.Versions = append(document.Versions, ModuleDocumentVersion{
			Version:     version.Record.Version,
			PublishedAt: *version.Record.PublishedAt,
			Href:        "/" + path.Join(versionPath, "index.json"),
		})

		ferret := ""
		if version.Manifest.Compatibility != nil {
			ferret = version.Manifest.Compatibility.Ferret
		}

		versionDocument := VersionDocument{
			SchemaVersion: registrydist.SchemaVersion,
			ID:            registryModule.ID(),
			Version:       version.Record.Version,
			Description:   version.Manifest.Description,
			Namespace:     version.Manifest.Namespace,
			Ferret:        ferret,
			License:       version.Manifest.License,
			Links:         cloneStringMap(version.Manifest.Links),
			Source: VersionSource{
				Repository: registryModule.Manifest.Source.Repository,
				Path:       registryModule.Manifest.Source.Path,
				Commit:     version.Record.Commit,
			},
			Package: VersionPackage{Path: version.PackagePath},
			Content: map[string]string{
				"documentation":     "./docs.md",
				"documentationHtml": "./docs.html",
			},
		}

		if err := distribution.addJSON(path.Join(versionPath, "index.json"), versionDocument); err != nil {
			return moduleProjection{}, err
		}

		distribution.Files[path.Join(versionPath, "docs.md")] = bytes.Clone(version.Documentation)

		renderedDocumentation, err := renderDocumentation(version.Documentation, version.Manifest.Documentation)
		if err != nil {
			return moduleProjection{}, fmt.Errorf("render documentation for %s@%s: %w", registryModule.ID(), version.Record.Version, err)
		}
		distribution.Files[path.Join(versionPath, "docs.html")] = renderedDocumentation
	}

	if err := distribution.addJSON(path.Join(modulePath, "index.json"), document); err != nil {
		return moduleProjection{}, err
	}

	return moduleProjection{
		indexEntry:  indexEntry,
		categoryIDs: uniqueCategoryIDs(metadataVersion.Manifest.Categories),
	}, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}

	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}

func validateCategoryIDs(moduleID, version string, categories []string) error {
	for _, categoryID := range categories {
		if !categoryIDPattern.MatchString(categoryID) {
			return fmt.Errorf("module %s@%s category %q is invalid: must match %s", moduleID, version, categoryID, categoryIDPatternText)
		}
	}

	return nil
}

func uniqueCategoryIDs(categories []string) []string {
	unique := make(map[string]struct{}, len(categories))

	for _, categoryID := range categories {
		unique[categoryID] = struct{}{}
	}

	return sortedDistributionPaths(unique)
}

func categoryDisplayName(categoryID string) string {
	words := strings.Split(categoryID, "-")

	for index, word := range words {
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}

	return strings.Join(words, " ")
}

func (distribution *Distribution) addJSON(relativePath string, document any) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", relativePath, err)
	}

	distribution.Files[relativePath] = append(data, '\n')

	return nil
}

func sortVersions(versions []*Version) error {
	parsed := make(map[*Version]*semver.Version, len(versions))

	for _, version := range versions {
		value, err := semver.StrictNewVersion(version.Record.Version)
		if err != nil {
			return err
		}
		parsed[version] = value
	}

	sort.Slice(versions, func(i, j int) bool {
		comparison := parsed[versions[i]].Compare(parsed[versions[j]])
		if comparison != 0 {
			return comparison > 0
		}

		return versions[i].Record.Version < versions[j].Record.Version
	})

	return nil
}

// WriteDistribution replaces dist/ with the complete generated distribution.
func WriteDistribution(root string, distribution *Distribution) error {
	if distribution == nil {
		return fmt.Errorf("distribution is nil")
	}

	staging, err := os.MkdirTemp(root, ".dist-staging-")
	if err != nil {
		return fmt.Errorf("create distribution staging directory: %w", err)
	}

	defer os.RemoveAll(staging)

	if err := os.Chmod(staging, 0o755); err != nil {
		return fmt.Errorf("set distribution staging permissions: %w", err)
	}

	paths := sortedDistributionPaths(distribution.Files)
	for _, relativePath := range paths {
		if err := validateDistributionPath(relativePath); err != nil {
			return err
		}

		filePath := filepath.Join(staging, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("create directory for %s: %w", relativePath, err)
		}

		if err := os.WriteFile(filePath, distribution.Files[relativePath], 0o644); err != nil {
			return fmt.Errorf("write %s: %w", relativePath, err)
		}
	}

	destination := filepath.Join(root, distributionPath)
	if _, err := os.Lstat(destination); os.IsNotExist(err) {
		if err := os.Rename(staging, destination); err != nil {
			return fmt.Errorf("install distribution: %w", err)
		}

		return nil
	} else if err != nil {
		return fmt.Errorf("inspect existing distribution: %w", err)
	}

	backupRoot, err := os.MkdirTemp(root, ".dist-replacement-")
	if err != nil {
		return fmt.Errorf("create distribution replacement directory: %w", err)
	}

	defer os.RemoveAll(backupRoot)

	backup := filepath.Join(backupRoot, "previous")
	if err := os.Rename(destination, backup); err != nil {
		return fmt.Errorf("move existing distribution aside: %w", err)
	}

	if err := os.Rename(staging, destination); err != nil {
		if restoreErr := os.Rename(backup, destination); restoreErr != nil {
			return fmt.Errorf("install distribution: %w (restore previous distribution: %v)", err, restoreErr)
		}

		return fmt.Errorf("install distribution: %w", err)
	}

	if err := os.RemoveAll(backupRoot); err != nil {
		return fmt.Errorf("remove previous distribution: %w", err)
	}

	return nil
}

// VerifyDistribution compares the complete generated dist/ tree with the expected distribution.
func VerifyDistribution(root string, distribution *Distribution) error {
	if distribution == nil {
		return fmt.Errorf("distribution is nil")
	}

	destination := filepath.Join(root, distributionPath)
	actual := make(map[string][]byte)
	err := filepath.WalkDir(destination, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if filePath == destination || entry.IsDir() {
			return nil
		}

		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("generated %s is not a regular file", filepath.ToSlash(filePath))
		}

		relativePath, err := filepath.Rel(destination, filePath)
		if err != nil {
			return err
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		actual[filepath.ToSlash(relativePath)] = data

		return nil
	})
	if err != nil {
		return fmt.Errorf("read generated distribution: %w", err)
	}

	for _, relativePath := range sortedDistributionPaths(distribution.Files) {
		current, exists := actual[relativePath]
		if !exists {
			return fmt.Errorf("dist/%s is missing; run barn generate", relativePath)
		}

		if !bytes.Equal(current, distribution.Files[relativePath]) {
			return fmt.Errorf("dist/%s is stale; run barn generate", relativePath)
		}

		delete(actual, relativePath)
	}

	if len(actual) != 0 {
		return fmt.Errorf("dist/%s is unexpected; run barn generate", sortedDistributionPaths(actual)[0])
	}

	return nil
}

func sortedDistributionPaths[T any](files map[string]T) []string {
	paths := make([]string, 0, len(files))

	for relativePath := range files {
		paths = append(paths, relativePath)
	}

	sort.Strings(paths)

	return paths
}

func validateDistributionPath(relativePath string) error {
	if relativePath == "" || strings.Contains(relativePath, "\\") || strings.HasPrefix(relativePath, "/") || path.Clean(relativePath) != relativePath || relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, "../") {
		return fmt.Errorf("invalid distribution path %q", relativePath)
	}

	return nil
}
