// Package barn validates Ferret Registry source state and generates its catalog.
package barn

import (
	modulemanifest "github.com/MontFerret/specs/pkg/module"
	registryspec "github.com/MontFerret/specs/pkg/registry"
)

type (
	// Registry contains validated registry source records.
	Registry struct {
		Root    string
		Modules []*Module
	}

	// Module contains one registry module and its versions.
	Module struct {
		Directory string
		Manifest  *registryspec.ModuleManifest
		Versions  []*Version
	}

	// Version contains one registry version record and its authoritative source manifest.
	Version struct {
		Path     string
		Record   *registryspec.VersionRecord
		Manifest *modulemanifest.Manifest
	}
)

// ID returns the module's canonical owner/name identity.
func (m *Module) ID() string {
	return m.Manifest.Owner + "/" + m.Manifest.Name
}
