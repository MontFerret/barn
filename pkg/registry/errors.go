package registry

import (
	"errors"
	"fmt"
)

var (
	ErrModuleNotFound    = errors.New("registry module not found")
	ErrVersionNotFound   = errors.New("registry version not found")
	ErrMalformedArtifact = errors.New("malformed registry artifact")
	ErrUnsupportedFormat = errors.New("unsupported registry format")
)

type (
	// HTTPError reports a non-successful response from the registry host.
	HTTPError struct {
		URL        string
		StatusCode int
		Status     string
	}

	// TransportError reports a failure to perform a registry HTTP request.
	TransportError struct {
		URL string
		Err error
	}

	// ArtifactError reports an invalid generated registry document.
	ArtifactError struct {
		URL string
		Err error
	}

	// UnsupportedFormatError reports a document with an unsupported schema version.
	UnsupportedFormatError struct {
		URL     string
		Version int
	}
)

func (e *HTTPError) Error() string {
	return fmt.Sprintf("registry request %s returned %s", e.URL, e.Status)
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("registry request %s failed: %v", e.URL, e.Err)
}

func (e *TransportError) Unwrap() error {
	return e.Err
}

func (e *ArtifactError) Error() string {
	return fmt.Sprintf("registry artifact %s is malformed: %v", e.URL, e.Err)
}

func (e *ArtifactError) Unwrap() error {
	return e.Err
}

func (e *ArtifactError) Is(target error) bool {
	return target == ErrMalformedArtifact
}

func (e *UnsupportedFormatError) Error() string {
	return fmt.Sprintf("registry artifact %s uses unsupported schema version %d", e.URL, e.Version)
}

func (e *UnsupportedFormatError) Unwrap() error {
	return ErrUnsupportedFormat
}
