package registry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	gomodule "golang.org/x/mod/module"
	"golang.org/x/sync/errgroup"

	"github.com/MontFerret/barn/internal/registrydist"
	registryspec "github.com/MontFerret/specs/pkg/registry"
)

const (
	maxArtifactSize     = 8 << 20
	maxSearchConcurrent = 6
)

var (
	moduleSegmentPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)
	categoryIDPattern    = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type (
	// Client consumes the generated static Ferret Registry distribution.
	Client struct {
		baseURL    *url.URL
		httpClient *http.Client
	}

	moduleReference struct {
		summary ModuleSummary
		href    *url.URL
	}

	versionReference struct {
		summary VersionSummary
		href    *url.URL
	}

	categoryReference struct {
		category Category
		href     *url.URL
	}
)

// NewClient creates a client for the generated static registry.
func NewClient(setters ...Option) (*Client, error) {
	options, err := defaultClientOptions()
	if err != nil {
		return nil, err
	}

	for _, setter := range setters {
		if setter == nil {
			continue
		}

		if err := setter(&options); err != nil {
			return nil, err
		}
	}

	return &Client{baseURL: options.baseURL, httpClient: options.httpClient}, nil
}

// Modules lists compact summaries of all registered modules.
func (client *Client) Modules(ctx context.Context) ([]ModuleSummary, error) {
	references, err := client.moduleReferences(ctx)
	if err != nil {
		return nil, err
	}

	return moduleSummaries(references), nil
}

// Module loads the complete public metadata for one canonical owner/name identity.
func (client *Client) Module(ctx context.Context, id string) (*Module, error) {
	module, _, err := client.loadModule(ctx, id)

	return module, err
}

// Versions lists the available versions of a module.
func (client *Client) Versions(ctx context.Context, id string) ([]VersionSummary, error) {
	_, references, err := client.loadModule(ctx, id)
	if err != nil {
		return nil, err
	}

	versions := make([]VersionSummary, len(references))
	for index, reference := range references {
		versions[index] = reference.summary
	}

	return versions, nil
}

// Version loads the complete public metadata for one module version.
func (client *Client) Version(ctx context.Context, id, version string) (*Version, error) {
	_, references, err := client.loadModule(ctx, id)
	if err != nil {
		return nil, err
	}

	var selected *versionReference
	for index := range references {
		if references[index].summary.Version == version {
			selected = &references[index]

			break
		}
	}

	if selected == nil {
		return nil, fmt.Errorf("%w: %s@%s", ErrVersionNotFound, id, version)
	}

	var document registrydist.VersionDocument
	if err := client.fetchJSON(ctx, selected.href, &document); err != nil {
		return nil, err
	}

	if err := validateSchemaVersion(selected.href, document.SchemaVersion); err != nil {
		return nil, err
	}

	if document.ID != id || document.Version != version {
		return nil, malformed(selected.href, "document identity %q@%q does not match requested %q@%q", document.ID, document.Version, id, version)
	}

	if document.Description == "" || document.Namespace == "" || document.License == "" || document.Package.Path == "" || document.Source.Repository == "" || document.Source.Commit == "" {
		return nil, malformed(selected.href, "version document is missing required metadata")
	}

	if err := gomodule.Check(document.Package.Path, "v"+document.Version); err != nil {
		return nil, malformed(selected.href, "package metadata is invalid: %v", err)
	}

	owner, name, _ := strings.Cut(id, "/")
	if err := registryspec.ValidateModuleManifest(&registryspec.ModuleManifest{
		Schema: registryspec.ModuleManifestSchemaV1,
		Owner:  owner,
		Name:   name,
		Source: registryspec.Source{
			Repository: document.Source.Repository,
			Path:       document.Source.Path,
		},
	}); err != nil {
		return nil, malformed(selected.href, "version source is invalid: %v", err)
	}

	if err := registryspec.ValidateVersionRecord(&registryspec.VersionRecord{
		Schema:  registryspec.VersionRecordSchemaV1,
		Version: document.Version,
		Tag:     "v" + document.Version,
		Commit:  document.Source.Commit,
	}); err != nil {
		return nil, malformed(selected.href, "version release identity is invalid: %v", err)
	}

	content := make(map[string]string, len(document.Content))
	for name, href := range document.Content {
		if name == "" {
			return nil, malformed(selected.href, "content name is empty")
		}

		resolved, err := client.resolveLink(selected.href, href)
		if err != nil {
			return nil, malformed(selected.href, "content %q: %v", name, err)
		}

		content[name] = resolved.String()
	}

	links := make(map[string]string, len(document.Links))
	for name, href := range document.Links {
		if name == "" {
			return nil, malformed(selected.href, "link name is empty")
		}

		link, err := url.Parse(href)
		if err != nil || link.Scheme != "https" || link.Host == "" || link.User != nil {
			return nil, malformed(selected.href, "link %q is not an absolute HTTPS URL", name)
		}

		links[name] = link.String()
	}

	return &Version{
		ID:          document.ID,
		Version:     document.Version,
		Description: document.Description,
		Namespace:   document.Namespace,
		Ferret:      document.Ferret,
		License:     document.License,
		Links:       links,
		Source: Source{
			Repository: document.Source.Repository,
			Path:       document.Source.Path,
			Commit:     document.Source.Commit,
		},
		Package: Package{Path: document.Package.Path},
		Content: content,
	}, nil
}

// Categories lists all category filters in the registry.
func (client *Client) Categories(ctx context.Context) ([]Category, error) {
	references, err := client.categoryReferences(ctx)
	if err != nil {
		return nil, err
	}

	categories := make([]Category, len(references))
	for index, reference := range references {
		categories[index] = reference.category
	}

	return categories, nil
}

// Search filters module summaries locally by canonical ID, description, and category.
func (client *Client) Search(ctx context.Context, options SearchOptions) ([]ModuleSummary, error) {
	query := strings.ToLower(strings.TrimSpace(options.Query))
	categoryID := strings.TrimSpace(options.Category)

	var references []moduleReference
	if categoryID == "" {
		var err error
		references, err = client.moduleReferences(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		categories, err := client.categoryReferences(ctx)
		if err != nil {
			return nil, err
		}

		var selected *categoryReference
		for index := range categories {
			if categories[index].category.ID == categoryID {
				selected = &categories[index]

				break
			}
		}

		if selected == nil {
			return []ModuleSummary{}, nil
		}

		var document registrydist.CategoryDocument
		if err := client.fetchJSON(ctx, selected.href, &document); err != nil {
			return nil, err
		}

		if err := validateSchemaVersion(selected.href, document.SchemaVersion); err != nil {
			return nil, err
		}

		if document.Category.ID != selected.category.ID || document.Category.Name != selected.category.Name {
			return nil, malformed(selected.href, "category identity does not match its index entry")
		}

		references, err = client.convertModuleEntries(selected.href, document.Modules)
		if err != nil {
			return nil, err
		}

		if len(references) != selected.category.Count {
			return nil, malformed(selected.href, "category module count is %d, want %d", len(references), selected.category.Count)
		}
	}

	if query == "" {
		return moduleSummaries(references), nil
	}

	matches := make([]bool, len(references))
	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(maxSearchConcurrent)

	for index := range references {
		if strings.Contains(strings.ToLower(references[index].summary.ID), query) {
			matches[index] = true

			continue
		}

		index := index
		group.Go(func() error {
			module, _, err := client.loadModuleReference(groupContext, &references[index])
			if err != nil {
				return err
			}

			matches[index] = strings.Contains(strings.ToLower(module.Description), query)

			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, err
	}

	filtered := make([]ModuleSummary, 0, len(references))
	for index, reference := range references {
		if matches[index] {
			filtered = append(filtered, reference.summary)
		}
	}

	return filtered, nil
}

func (client *Client) loadModule(ctx context.Context, id string) (*Module, []versionReference, error) {
	references, err := client.moduleReferences(ctx)
	if err != nil {
		return nil, nil, err
	}

	var selected *moduleReference
	for index := range references {
		if references[index].summary.ID == id {
			selected = &references[index]
			break
		}
	}

	if selected == nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrModuleNotFound, id)
	}

	return client.loadModuleReference(ctx, selected)
}

func (client *Client) loadModuleReference(ctx context.Context, selected *moduleReference) (*Module, []versionReference, error) {
	var document registrydist.ModuleDocument
	if err := client.fetchJSON(ctx, selected.href, &document); err != nil {
		return nil, nil, err
	}

	if err := validateSchemaVersion(selected.href, document.SchemaVersion); err != nil {
		return nil, nil, err
	}

	if document.ID != selected.summary.ID || document.Owner+"/"+document.Name != selected.summary.ID {
		return nil, nil, malformed(selected.href, "module identity does not match requested %q", selected.summary.ID)
	}

	if document.Latest != selected.summary.Latest {
		return nil, nil, malformed(selected.href, "module latest version does not match its index entry")
	}

	versions, err := client.convertVersionEntries(selected.href, document.Versions)
	if err != nil {
		return nil, nil, err
	}

	if len(versions) == 0 {
		return nil, nil, malformed(selected.href, "module has no versions")
	}

	if document.Latest != "" && !containsVersion(versions, document.Latest) {
		return nil, nil, malformed(selected.href, "latest version %q is not listed", document.Latest)
	}

	moduleVersions := make([]VersionSummary, len(versions))
	for index, reference := range versions {
		moduleVersions[index] = reference.summary
	}

	return &Module{
		ID:          document.ID,
		Owner:       document.Owner,
		Name:        document.Name,
		Description: document.Description,
		Latest:      document.Latest,
		Versions:    moduleVersions,
	}, versions, nil
}

func (client *Client) moduleReferences(ctx context.Context) ([]moduleReference, error) {
	root, rootURL, err := client.rootIndex(ctx)
	if err != nil {
		return nil, err
	}

	href, exists := root.Artifacts["modules"]
	if !exists || href == "" {
		return nil, malformed(rootURL, "root index does not define the modules artifact")
	}

	indexURL, err := client.resolveLink(rootURL, href)
	if err != nil {
		return nil, malformed(rootURL, "modules artifact: %v", err)
	}

	var index registrydist.ModuleIndex
	if err := client.fetchJSON(ctx, indexURL, &index); err != nil {
		return nil, err
	}

	if err := validateSchemaVersion(indexURL, index.SchemaVersion); err != nil {
		return nil, err
	}

	return client.convertModuleEntries(indexURL, index.Modules)
}

func (client *Client) convertModuleEntries(parent *url.URL, entries []registrydist.ModuleIndexEntry) ([]moduleReference, error) {
	references := make([]moduleReference, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))

	for _, entry := range entries {
		if !validModuleID(entry.ID) {
			return nil, malformed(parent, "module ID %q is invalid", entry.ID)
		}

		if _, exists := seen[entry.ID]; exists {
			return nil, malformed(parent, "module ID %q is duplicated", entry.ID)
		}

		seen[entry.ID] = struct{}{}

		if entry.Latest != "" {
			if _, err := semver.StrictNewVersion(entry.Latest); err != nil {
				return nil, malformed(parent, "module %q latest version %q is invalid", entry.ID, entry.Latest)
			}
		}

		href, err := client.resolveLink(parent, entry.Href)
		if err != nil {
			return nil, malformed(parent, "module %q link: %v", entry.ID, err)
		}

		references = append(references, moduleReference{
			summary: ModuleSummary{ID: entry.ID, Latest: entry.Latest},
			href:    href,
		})
	}

	sort.Slice(references, func(i, j int) bool { return references[i].summary.ID < references[j].summary.ID })

	return references, nil
}

func (client *Client) convertVersionEntries(parent *url.URL, entries []registrydist.ModuleDocumentVersion) ([]versionReference, error) {
	references := make([]versionReference, 0, len(entries))
	parsed := make(map[string]*semver.Version, len(entries))

	for _, entry := range entries {
		if _, exists := parsed[entry.Version]; exists {
			return nil, malformed(parent, "version %q is duplicated", entry.Version)
		}

		value, err := semver.StrictNewVersion(entry.Version)
		if err != nil {
			return nil, malformed(parent, "version %q is invalid", entry.Version)
		}

		parsed[entry.Version] = value
		_, offset := entry.PublishedAt.Zone()
		if entry.PublishedAt.IsZero() || offset != 0 {
			return nil, malformed(parent, "version %q publication timestamp is missing or is not UTC", entry.Version)
		}

		href, err := client.resolveLink(parent, entry.Href)
		if err != nil {
			return nil, malformed(parent, "version %q link: %v", entry.Version, err)
		}

		references = append(references, versionReference{
			summary: VersionSummary{Version: entry.Version, PublishedAt: entry.PublishedAt},
			href:    href,
		})
	}

	sort.Slice(references, func(i, j int) bool {
		comparison := parsed[references[i].summary.Version].Compare(parsed[references[j].summary.Version])
		if comparison != 0 {
			return comparison > 0
		}

		return references[i].summary.Version < references[j].summary.Version
	})

	return references, nil
}

func (client *Client) categoryReferences(ctx context.Context) ([]categoryReference, error) {
	root, rootURL, err := client.rootIndex(ctx)
	if err != nil {
		return nil, err
	}

	href, exists := root.Artifacts["categories"]
	if !exists || href == "" {
		return nil, malformed(rootURL, "root index does not define the categories artifact")
	}

	indexURL, err := client.resolveLink(rootURL, href)
	if err != nil {
		return nil, malformed(rootURL, "categories artifact: %v", err)
	}

	var index registrydist.CategoryIndex
	if err := client.fetchJSON(ctx, indexURL, &index); err != nil {
		return nil, err
	}

	if err := validateSchemaVersion(indexURL, index.SchemaVersion); err != nil {
		return nil, err
	}

	references := make([]categoryReference, 0, len(index.Categories))
	seen := make(map[string]struct{}, len(index.Categories))

	for _, entry := range index.Categories {
		if !categoryIDPattern.MatchString(entry.ID) || entry.Name == "" || entry.Count < 0 {
			return nil, malformed(indexURL, "category %q has invalid metadata", entry.ID)
		}

		if _, exists := seen[entry.ID]; exists {
			return nil, malformed(indexURL, "category ID %q is duplicated", entry.ID)
		}

		seen[entry.ID] = struct{}{}

		href, err := client.resolveLink(indexURL, entry.Href)
		if err != nil {
			return nil, malformed(indexURL, "category %q link: %v", entry.ID, err)
		}

		references = append(references, categoryReference{
			category: Category{ID: entry.ID, Name: entry.Name, Count: entry.Count},
			href:     href,
		})
	}

	sort.Slice(references, func(i, j int) bool { return references[i].category.ID < references[j].category.ID })

	return references, nil
}

func (client *Client) rootIndex(ctx context.Context) (*registrydist.RootIndex, *url.URL, error) {
	rootURL := client.baseURL.ResolveReference(&url.URL{Path: "index.json"})
	var root registrydist.RootIndex

	if err := client.fetchJSON(ctx, rootURL, &root); err != nil {
		return nil, nil, err
	}

	if err := validateSchemaVersion(rootURL, root.SchemaVersion); err != nil {
		return nil, nil, err
	}

	return &root, rootURL, nil
}

func (client *Client) fetchJSON(ctx context.Context, endpoint *url.URL, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return &TransportError{URL: endpoint.String(), Err: err}
	}

	request.Header.Set("Accept", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return &TransportError{URL: endpoint.String(), Err: err}
	}

	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))

		return &HTTPError{URL: endpoint.String(), StatusCode: response.StatusCode, Status: response.Status}
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxArtifactSize+1))
	if err != nil {
		return &TransportError{URL: endpoint.String(), Err: err}
	}

	if len(data) > maxArtifactSize {
		return malformed(endpoint, "document exceeds %d bytes", maxArtifactSize)
	}

	if err := registrydist.Decode(data, target); err != nil {
		return &ArtifactError{URL: endpoint.String(), Err: err}
	}

	return nil
}

func (client *Client) resolveLink(parent *url.URL, href string) (*url.URL, error) {
	reference, err := url.Parse(href)
	if err != nil {
		return nil, fmt.Errorf("parse link: %w", err)
	}

	resolved := parent.ResolveReference(reference)
	if (resolved.Scheme != "http" && resolved.Scheme != "https") || resolved.Host == "" {
		return nil, fmt.Errorf("link must resolve to an absolute HTTP or HTTPS URL")
	}

	if resolved.User != nil || resolved.RawQuery != "" || resolved.Fragment != "" {
		return nil, fmt.Errorf("link must not contain credentials, a query, or a fragment")
	}

	if !strings.EqualFold(resolved.Scheme, client.baseURL.Scheme) || !strings.EqualFold(resolved.Host, client.baseURL.Host) {
		return nil, fmt.Errorf("link resolves outside the configured registry origin")
	}

	return resolved, nil
}

func validateSchemaVersion(endpoint *url.URL, version int) error {
	if version == 0 {
		return malformed(endpoint, "schemaVersion is required")
	}

	if version != registrydist.SchemaVersion {
		return &UnsupportedFormatError{URL: endpoint.String(), Version: version}
	}

	return nil
}

func malformed(endpoint *url.URL, format string, arguments ...any) error {
	return &ArtifactError{URL: endpoint.String(), Err: fmt.Errorf(format, arguments...)}
}

func moduleSummaries(references []moduleReference) []ModuleSummary {
	summaries := make([]ModuleSummary, len(references))
	for index, reference := range references {
		summaries[index] = reference.summary
	}

	return summaries
}

func validModuleID(id string) bool {
	owner, name, found := strings.Cut(id, "/")

	return found && moduleSegmentPattern.MatchString(owner) && moduleSegmentPattern.MatchString(name)
}

func containsVersion(references []versionReference, version string) bool {
	for _, reference := range references {
		if reference.summary.Version == version {
			return true
		}
	}

	return false
}
