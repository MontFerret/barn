package publish

import (
	"errors"
	"fmt"
)

var (
	ErrRepositoryRequired      = errors.New("module repository metadata is required")
	ErrVersionAlreadyPublished = errors.New("module version is already published")
	ErrSourceMismatch          = errors.New("module source does not match the registry")
)

type (
	// Stage identifies the publication preparation phase that failed.
	Stage string

	// Error adds a stable preparation stage while preserving the underlying error.
	Error struct {
		Stage Stage
		Err   error
	}
)

const (
	StageManifest Stage = "manifest"
	StageRequest  Stage = "request"
	StageRegistry Stage = "registry"
	StageGit      Stage = "git"
	StageAPI      Stage = "api"
	StageFiles    Stage = "files"
)

func (e *Error) Error() string {
	return fmt.Sprintf("prepare publication at %s stage: %v", e.Stage, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func stageError(stage Stage, err error) error {
	return &Error{Stage: stage, Err: err}
}
