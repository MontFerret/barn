// Package registrydist defines the internal wire representation of the
// generated Ferret Registry distribution.
package registrydist

import "encoding/json"

const SchemaVersion = 1

type (
	RootIndex struct {
		SchemaVersion int               `json:"schemaVersion"`
		Artifacts     map[string]string `json:"artifacts"`
	}

	ModuleIndex struct {
		SchemaVersion int                `json:"schemaVersion"`
		Modules       []ModuleIndexEntry `json:"modules"`
	}

	ModuleIndexEntry struct {
		ID     string `json:"id"`
		Latest string `json:"latest,omitempty"`
		Href   string `json:"href"`
	}

	CategoryIndex struct {
		SchemaVersion int                  `json:"schemaVersion"`
		Categories    []CategoryIndexEntry `json:"categories"`
	}

	CategoryIndexEntry struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Count int    `json:"count"`
		Href  string `json:"href"`
	}

	CategoryDocument struct {
		SchemaVersion int                `json:"schemaVersion"`
		Category      CategorySummary    `json:"category"`
		Modules       []ModuleIndexEntry `json:"modules"`
	}

	CategorySummary struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	PluginIndex struct {
		SchemaVersion int               `json:"schemaVersion"`
		Plugins       []json.RawMessage `json:"plugins"`
	}

	ModuleDocument struct {
		SchemaVersion int                     `json:"schemaVersion"`
		ID            string                  `json:"id"`
		Owner         string                  `json:"owner"`
		Name          string                  `json:"name"`
		Description   string                  `json:"description"`
		Latest        string                  `json:"latest,omitempty"`
		Versions      []ModuleDocumentVersion `json:"versions"`
	}

	ModuleDocumentVersion struct {
		Version string `json:"version"`
		Href    string `json:"href"`
	}

	VersionDocument struct {
		SchemaVersion int               `json:"schemaVersion"`
		ID            string            `json:"id"`
		Version       string            `json:"version"`
		Description   string            `json:"description"`
		Namespace     string            `json:"namespace"`
		Ferret        string            `json:"ferret,omitempty"`
		License       string            `json:"license"`
		Links         map[string]string `json:"links,omitempty"`
		Source        VersionSource     `json:"source"`
		Package       VersionPackage    `json:"package"`
		Content       map[string]string `json:"content"`
	}

	VersionPackage struct {
		Path string `json:"path"`
	}

	VersionSource struct {
		Repository string `json:"repository"`
		Path       string `json:"path,omitempty"`
		Commit     string `json:"commit"`
	}
)
