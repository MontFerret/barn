// Package apiref statically derives Ferret-facing API Reference artifacts from Go source.
package apiref

import (
	"fmt"
	"go/token"
)

type (
	// ErrorKind identifies a stable API analysis failure category.
	ErrorKind string

	// AnalysisError reports why a complete API Reference could not be proven.
	AnalysisError struct {
		Kind     ErrorKind
		ModuleID string
		Version  string
		Position token.Position
		Err      error
	}
)

const (
	ErrorDeclarationNotFound     ErrorKind = "declaration-not-found"
	ErrorUnsupportedRegistration ErrorKind = "unsupported-registration"
	ErrorInvalidPackage          ErrorKind = "invalid-package"
	ErrorInvalidDocumentation    ErrorKind = "invalid-documentation"
	ErrorUnresolvedTarget        ErrorKind = "unresolved-target"
	ErrorInternal                ErrorKind = "internal"
)

func (e *AnalysisError) Error() string {
	if e == nil {
		return "API analysis failed"
	}

	location := ""
	if e.Position.IsValid() {
		location = " at " + e.Position.String()
	}

	return fmt.Sprintf("API analysis %s for %s@%s%s: %v", e.Kind, e.ModuleID, e.Version, location, e.Err)
}

func (e *AnalysisError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}
