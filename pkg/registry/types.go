// Package registry provides a client for the generated, statically hosted
// Ferret Registry distribution.
package registry

const DefaultBaseURL = "https://registry.ferretlang.org"

type (
	// ModuleSummary is the compact module representation used by indexes and search.
	ModuleSummary struct {
		ID     string
		Latest string
	}

	// Module contains the public metadata and available versions of one module.
	Module struct {
		ID          string
		Owner       string
		Name        string
		Description string
		Latest      string
		Versions    []VersionSummary
	}

	// VersionSummary identifies an available module version.
	VersionSummary struct {
		Version string
	}

	// Version contains the immutable public metadata for one module version.
	// Content values are absolute URLs resolved by the client.
	Version struct {
		ID        string
		Version   string
		Namespace string
		Ferret    string
		Source    Source
		Content   map[string]string
	}

	// Source identifies the immutable Git source of a module version.
	Source struct {
		Repository string
		Path       string
		Commit     string
	}

	// Category describes a registry category available for search filtering.
	Category struct {
		ID    string
		Name  string
		Count int
	}

	// SearchOptions controls deterministic filtering by ID, description, and category.
	SearchOptions struct {
		Query    string
		Category string
	}
)
