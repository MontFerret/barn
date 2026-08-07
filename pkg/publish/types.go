// Package publish prepares validated, Git/PR-oriented Ferret Registry submissions.
package publish

import (
	registryclient "github.com/MontFerret/barn/pkg/registry"
	registryspec "github.com/MontFerret/specs/pkg/registry"
)

type (
	// Kind describes the registry change represented by a prepared result.
	Kind string

	// Request identifies a local module release to prepare for publication.
	Request struct {
		Directory string
		Tag       string
		Registry  *registryclient.Client
	}

	// Result is a validated, deterministic Barn registry submission.
	Result struct {
		Kind    Kind
		Module  *registryspec.ModuleManifest
		Version *registryspec.VersionRecord
		Files   []File
	}

	// File is one Barn-repository-relative source record to submit.
	File struct {
		Path    string
		Content []byte
	}
)

const (
	NewModule  Kind = "new-module"
	NewVersion Kind = "new-version"
)
