package barn

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/MontFerret/specs/pkg/api"
	modulemanifest "github.com/MontFerret/specs/pkg/module"
	registryartifact "github.com/MontFerret/specs/pkg/registry/artifact"
)

var errLegacyDistribution = errors.New("published distribution does not contain source provenance")

type cachedDistribution struct {
	distribution *Distribution
	sourceCommit string
}

func readCachedDistribution(directory string) (*cachedDistribution, error) {
	distribution, err := readDistributionPath(directory)
	if err != nil {
		return nil, err
	}

	rootData, exists := distribution.Files["index.json"]
	if !exists {
		return nil, fmt.Errorf("published distribution index.json is missing")
	}

	root, err := registryartifact.ParseRootIndex(rootData)
	if err != nil {
		if isLegacyRootIndex(rootData) {
			return nil, errLegacyDistribution
		}

		return nil, fmt.Errorf("parse published index.json: %w", err)
	}

	return &cachedDistribution{distribution: distribution, sourceCommit: root.Source.Commit}, nil
}

func isLegacyRootIndex(data []byte) bool {
	var legacy struct {
		SchemaVersion int               `json:"schemaVersion"`
		Artifacts     map[string]string `json:"artifacts"`
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&legacy); err != nil {
		return false
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return false
	}

	if legacy.SchemaVersion != registryartifact.SchemaVersion {
		return false
	}

	root := &registryartifact.RootIndex{
		SchemaVersion: registryartifact.SchemaVersion,
		Source:        registryartifact.RootSource{Commit: strings.Repeat("0", 40)},
		Artifacts:     legacy.Artifacts,
	}

	return registryartifact.ValidateRootIndex(root) == nil
}

func readDistributionPath(directory string) (*Distribution, error) {
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, fmt.Errorf("inspect distribution directory: %w", err)
	}

	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("distribution path %s is not a real directory", directory)
	}

	distribution := &Distribution{Files: make(map[string][]byte)}
	err = filepath.WalkDir(directory, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relative, err := filepath.Rel(directory, filePath)
		if err != nil {
			return err
		}

		relative = filepath.ToSlash(relative)
		if relative == "." {
			return nil
		}

		if relative == ".git" {
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("published .git is a symbolic link")
			}

			if entry.IsDir() {
				return filepath.SkipDir
			}

			if entry.Type().IsRegular() {
				return nil
			}

			return fmt.Errorf("published .git is not a regular file or directory")
		}

		if entry.IsDir() {
			return nil
		}

		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("published %s is not a regular file", relative)
		}

		if relative == ".nojekyll" || relative == "CNAME" {
			return nil
		}

		if err := validateDistributionPath(relative); err != nil {
			return err
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		distribution.Files[relative] = data

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read published distribution: %w", err)
	}

	return distribution, nil
}

func hydrateRegistryFromDistribution(registry *Registry, cached *cachedDistribution, affected map[string]struct{}) error {
	if registry == nil {
		return fmt.Errorf("registry is nil")
	}

	if cached == nil || cached.distribution == nil {
		return fmt.Errorf("cached distribution is nil")
	}

	files := cached.distribution.Files
	used := map[string]struct{}{"index.json": {}}

	root, err := registryartifact.ParseRootIndex(files["index.json"])
	if err != nil {
		return fmt.Errorf("parse published index.json: %w", err)
	}

	if root.Source.Commit != cached.sourceCommit {
		return fmt.Errorf("published index.json source commit changed during validation")
	}

	wantArtifacts := map[string]string{
		registryartifact.ArtifactKeyCategories: "/categories.json",
		registryartifact.ArtifactKeyModules:    "/modules/index.json",
		registryartifact.ArtifactKeyPlugins:    "/plugins/index.json",
	}
	if !reflect.DeepEqual(root.Artifacts, wantArtifacts) {
		return fmt.Errorf("published index.json artifact links are not canonical")
	}

	plugins, err := parseCachedArtifact(files, used, "plugins/index.json", registryartifact.ParsePluginIndex)
	if err != nil {
		return err
	}

	if len(plugins.Plugins) != 0 {
		return fmt.Errorf("published plugin index is not empty")
	}

	moduleIndex, err := parseCachedArtifact(files, used, "modules/index.json", registryartifact.ParseModuleIndex)
	if err != nil {
		return err
	}

	currentModules := make(map[string]*Module, len(registry.Modules))
	for _, module := range registry.Modules {
		currentModules[module.ID()] = module
	}

	indexEntries := make(map[string]ModuleIndexEntry, len(moduleIndex.Modules))
	for _, entry := range moduleIndex.Modules {
		module, exists := currentModules[entry.ID]
		if !exists {
			return fmt.Errorf("published module %s was deleted from canonical source", entry.ID)
		}

		wantHref := "/" + path.Join("modules", module.Manifest.Owner, module.Manifest.Name, "index.json")
		if entry.Href != wantHref {
			return fmt.Errorf("published module %s href is %q, want %q", entry.ID, entry.Href, wantHref)
		}

		indexEntries[entry.ID] = entry
	}

	categoryMembership, err := loadCachedCategories(files, used, indexEntries)
	if err != nil {
		return err
	}

	for _, entry := range moduleIndex.Modules {
		module := currentModules[entry.ID]
		if err := hydrateCachedModule(files, used, module, entry, categoryMembership[entry.ID], affected); err != nil {
			return err
		}
	}

	for _, module := range registry.Modules {
		if _, exists := indexEntries[module.ID()]; exists {
			continue
		}

		for _, version := range module.Versions {
			if _, selected := affected[releaseKey(module.ID(), version.Record.Version)]; !selected {
				return fmt.Errorf("module %s@%s is missing from published state but is not affected", module.ID(), version.Record.Version)
			}
		}
	}

	for relative := range files {
		if _, exists := used[relative]; !exists {
			return fmt.Errorf("published %s is unexpected", relative)
		}
	}

	return nil
}

func hydrateCachedModule(files map[string][]byte, used map[string]struct{}, module *Module, entry ModuleIndexEntry, categories []string, affected map[string]struct{}) error {
	modulePath := path.Join("modules", module.Manifest.Owner, module.Manifest.Name)
	documentPath := path.Join(modulePath, "index.json")
	document, err := parseCachedArtifact(files, used, documentPath, registryartifact.ParseModuleDocument)
	if err != nil {
		return err
	}

	if document.ID != module.ID() || document.Owner != module.Manifest.Owner || document.Name != module.Manifest.Name || document.Latest != entry.Latest {
		return fmt.Errorf("published module document %s is inconsistent with its source or index", module.ID())
	}

	currentVersions := make(map[string]*Version, len(module.Versions))
	for _, version := range module.Versions {
		currentVersions[version.Record.Version] = version
	}

	cachedVersions := make([]*Version, 0, len(document.Versions))
	listedVersions := make(map[string]struct{}, len(document.Versions))
	for _, summary := range document.Versions {
		version, exists := currentVersions[summary.Version]
		if !exists {
			return fmt.Errorf("published module %s@%s was deleted from canonical source", module.ID(), summary.Version)
		}

		if version.Record.PublishedAt == nil || !version.Record.PublishedAt.Equal(summary.PublishedAt) {
			return fmt.Errorf("published timestamp for %s@%s does not match canonical source", module.ID(), summary.Version)
		}

		versionPath := path.Join(modulePath, "versions", summary.Version)
		wantHref := "/" + path.Join(versionPath, "index.json")
		if summary.Href != wantHref {
			return fmt.Errorf("published module %s@%s href is %q, want %q", module.ID(), summary.Version, summary.Href, wantHref)
		}

		versionDocument, err := parseCachedArtifact(files, used, path.Join(versionPath, "index.json"), registryartifact.ParseVersionDocument)
		if err != nil {
			return err
		}

		if versionDocument.ID != module.ID() || versionDocument.Version != summary.Version || versionDocument.Source.Repository != module.Manifest.Source.Repository || versionDocument.Source.Path != module.Manifest.Source.Path || versionDocument.Source.Commit != version.Record.Commit {
			return fmt.Errorf("published version document for %s@%s does not match canonical source", module.ID(), summary.Version)
		}

		wantContent := map[string]string{
			registryartifact.ContentKeyAPI:               "./api.json",
			registryartifact.ContentKeyDocumentation:     "./docs.md",
			registryartifact.ContentKeyDocumentationHTML: "./docs.html",
		}
		if !reflect.DeepEqual(versionDocument.Content, wantContent) {
			return fmt.Errorf("published content links for %s@%s are not canonical", module.ID(), summary.Version)
		}

		apiPath := path.Join(versionPath, "api.json")
		apiReference, err := parseCachedArtifact(files, used, apiPath, api.Parse)
		if err != nil {
			return err
		}

		if apiReference.ID != module.ID() || apiReference.Version != summary.Version {
			return fmt.Errorf("published API Reference for %s@%s has inconsistent identity", module.ID(), summary.Version)
		}

		docsPath := path.Join(versionPath, "docs.md")
		docs, err := cachedFile(files, used, docsPath)
		if err != nil {
			return err
		}

		htmlPath := path.Join(versionPath, "docs.html")
		if _, err := cachedFile(files, used, htmlPath); err != nil {
			return err
		}

		manifest := cachedManifest(module.ID(), versionDocument)
		version.Manifest = manifest
		version.PackagePath = versionDocument.Package.Path
		version.Documentation = bytes.Clone(docs)
		version.API = apiReference

		if _, selected := affected[releaseKey(module.ID(), summary.Version)]; !selected {
			version.Artifacts = map[string][]byte{
				path.Join(versionPath, "index.json"): bytes.Clone(files[path.Join(versionPath, "index.json")]),
				apiPath:                              bytes.Clone(files[apiPath]),
				docsPath:                             bytes.Clone(files[docsPath]),
				htmlPath:                             bytes.Clone(files[htmlPath]),
			}
		}

		cachedVersions = append(cachedVersions, version)
		listedVersions[summary.Version] = struct{}{}
	}

	for _, version := range module.Versions {
		if _, listed := listedVersions[version.Record.Version]; !listed {
			if _, selected := affected[releaseKey(module.ID(), version.Record.Version)]; !selected {
				return fmt.Errorf("module %s@%s is missing from published state but is not affected", module.ID(), version.Record.Version)
			}
		}
	}

	metadataVersion, latest, err := selectMetadataVersion(cachedVersions)
	if err != nil {
		return fmt.Errorf("select cached metadata for %s: %w", module.ID(), err)
	}

	if latest != document.Latest || metadataVersion.Manifest.Description != document.Description {
		return fmt.Errorf("published module metadata for %s is inconsistent with its versions", module.ID())
	}

	metadataVersion.Manifest.Categories = append([]string(nil), categories...)

	return nil
}

func loadCachedCategories(files map[string][]byte, used map[string]struct{}, indexEntries map[string]ModuleIndexEntry) (map[string][]string, error) {
	index, err := parseCachedArtifact(files, used, "categories.json", registryartifact.ParseCategoryIndex)
	if err != nil {
		return nil, err
	}

	membership := make(map[string][]string)
	for _, entry := range index.Categories {
		categoryPath := path.Join("categories", entry.ID+".json")
		if entry.Href != "/"+categoryPath {
			return nil, fmt.Errorf("published category %s href is not canonical", entry.ID)
		}

		document, err := parseCachedArtifact(files, used, categoryPath, registryartifact.ParseCategoryDocument)
		if err != nil {
			return nil, err
		}

		if document.Category.ID != entry.ID || document.Category.Name != entry.Name || len(document.Modules) != entry.Count {
			return nil, fmt.Errorf("published category %s is inconsistent with its index", entry.ID)
		}

		for _, module := range document.Modules {
			indexed, exists := indexEntries[module.ID]
			if !exists || indexed != module {
				return nil, fmt.Errorf("published category %s contains inconsistent module %s", entry.ID, module.ID)
			}

			membership[module.ID] = append(membership[module.ID], entry.ID)
		}
	}

	for moduleID := range membership {
		sort.Strings(membership[moduleID])
	}

	return membership, nil
}

func cachedManifest(moduleID string, document *registryartifact.VersionDocument) *modulemanifest.Manifest {
	manifest := &modulemanifest.Manifest{
		Schema:        modulemanifest.SchemaV1,
		Name:          moduleID,
		Namespace:     document.Namespace,
		Version:       document.Version,
		Description:   document.Description,
		License:       document.License,
		Documentation: "https://cached.invalid/",
		Links:         cloneStringMap(document.Links),
	}

	if document.Ferret != "" {
		manifest.Compatibility = &modulemanifest.Compatibility{Ferret: document.Ferret}
	}

	return manifest
}

func selectMetadataVersion(versions []*Version) (*Version, string, error) {
	ordered := append([]*Version(nil), versions...)
	if len(ordered) == 0 {
		return nil, "", fmt.Errorf("module has no versions")
	}

	if err := sortVersions(ordered); err != nil {
		return nil, "", err
	}

	metadata := ordered[0]
	latest := ""
	for _, version := range ordered {
		parsed, err := semver.StrictNewVersion(version.Record.Version)
		if err != nil {
			return nil, "", err
		}

		if parsed.Prerelease() != "" {
			continue
		}

		latest = version.Record.Version
		metadata = version

		break
	}

	return metadata, latest, nil
}

func parseCachedArtifact[T any](files map[string][]byte, used map[string]struct{}, relative string, parse func([]byte) (*T, error)) (*T, error) {
	data, err := cachedFile(files, used, relative)
	if err != nil {
		return nil, err
	}

	document, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse published %s: %w", relative, err)
	}

	return document, nil
}

func cachedFile(files map[string][]byte, used map[string]struct{}, relative string) ([]byte, error) {
	data, exists := files[relative]
	if !exists {
		return nil, fmt.Errorf("published %s is missing", relative)
	}

	used[relative] = struct{}{}

	return data, nil
}

func releaseKey(moduleID, version string) string {
	return moduleID + "@" + version
}
