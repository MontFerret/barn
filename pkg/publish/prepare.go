package publish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MontFerret/barn/internal/barn"
	registryclient "github.com/MontFerret/barn/pkg/registry"
	modulemanifest "github.com/MontFerret/specs/pkg/module"
	registryspec "github.com/MontFerret/specs/pkg/registry"
)

const placeholderCommit = "0000000000000000000000000000000000000000"

type (
	registryReader interface {
		Module(context.Context, string) (*registryclient.Module, error)
		Version(context.Context, string, string) (*registryclient.Version, error)
	}

	releaseInspector interface {
		Inspect(context.Context, registryspec.Source, string, string, string) (*barn.ResolvedRelease, error)
	}
)

// Prepare validates a local module release and returns the canonical Barn
// source records needed to publish it. Prepare does not write files or submit a
// pull request.
func Prepare(ctx context.Context, request Request) (*Result, error) {
	reader := request.Registry
	if reader == nil {
		var err error
		reader, err = registryclient.NewClient()
		if err != nil {
			return nil, stageError(StageRegistry, err)
		}
	}

	return prepare(ctx, request, reader, barn.GitInspector{})
}

func prepare(ctx context.Context, request Request, reader registryReader, inspector releaseInspector) (*Result, error) {
	directory := request.Directory
	if directory == "" {
		directory = "."
	}

	manifest, err := modulemanifest.LoadFile(filepath.Join(directory, modulemanifest.ManifestFilename))
	if err != nil {
		return nil, stageError(StageManifest, err)
	}

	if manifest.Repository == nil {
		return nil, stageError(StageRequest, ErrRepositoryRequired)
	}

	owner, name, valid := splitModuleID(manifest.Name)
	if !valid {
		return nil, stageError(StageRequest, fmt.Errorf("module name %q must be a canonical owner/name identity", manifest.Name))
	}

	moduleRecord := &registryspec.ModuleManifest{
		Schema: registryspec.ModuleManifestSchemaV1,
		Owner:  owner,
		Name:   name,
		Source: registryspec.Source{
			Repository: manifest.Repository.URL,
			Path:       manifest.Repository.Directory,
		},
	}

	if err := registryspec.ValidateModuleManifest(moduleRecord); err != nil {
		return nil, stageError(StageRequest, err)
	}

	requestedVersion := &registryspec.VersionRecord{
		Schema:  registryspec.VersionRecordSchemaV1,
		Version: manifest.Version,
		Tag:     request.Tag,
		Commit:  placeholderCommit,
	}

	if err := registryspec.ValidateVersionRecord(requestedVersion); err != nil {
		return nil, stageError(StageRequest, err)
	}

	kind, err := classify(ctx, reader, manifest.Name, manifest.Version, moduleRecord.Source)
	if err != nil {
		return nil, stageError(StageRegistry, err)
	}

	release, err := inspector.Inspect(ctx, moduleRecord.Source, manifest.Name, manifest.Version, request.Tag)
	if err != nil {
		return nil, stageError(StageGit, err)
	}

	versionRecord := &registryspec.VersionRecord{
		Schema:  registryspec.VersionRecordSchemaV1,
		Version: manifest.Version,
		Tag:     request.Tag,
		Commit:  release.Commit,
	}

	if err := registryspec.ValidateVersionRecord(versionRecord); err != nil {
		return nil, stageError(StageGit, err)
	}

	files, err := prepareFiles(kind, moduleRecord, versionRecord)
	if err != nil {
		return nil, stageError(StageFiles, err)
	}

	return &Result{
		Kind:    kind,
		Module:  moduleRecord,
		Version: versionRecord,
		Files:   files,
	}, nil
}

func classify(ctx context.Context, reader registryReader, moduleID, version string, source registryspec.Source) (Kind, error) {
	module, err := reader.Module(ctx, moduleID)
	if err != nil {
		if errors.Is(err, registryclient.ErrModuleNotFound) {
			return NewModule, nil
		}

		return "", err
	}

	for _, available := range module.Versions {
		if available.Version == version {
			return "", fmt.Errorf("%w: %s@%s", ErrVersionAlreadyPublished, moduleID, version)
		}
	}

	if len(module.Versions) == 0 {
		return "", fmt.Errorf("existing module %s has no versions", moduleID)
	}

	existing, err := reader.Version(ctx, moduleID, module.Versions[0].Version)
	if err != nil {
		return "", err
	}

	if existing.Source.Repository != source.Repository || existing.Source.Path != source.Path {
		return "", fmt.Errorf("%w: %s uses %s at %q, requested %s at %q", ErrSourceMismatch, moduleID, existing.Source.Repository, existing.Source.Path, source.Repository, source.Path)
	}

	return NewVersion, nil
}

func prepareFiles(kind Kind, module *registryspec.ModuleManifest, version *registryspec.VersionRecord) ([]File, error) {
	modulePath := path.Join("registry", "modules", module.Owner, module.Name)
	files := make([]File, 0, 2)

	if kind == NewModule {
		content, err := encodeRecord(module)
		if err != nil {
			return nil, fmt.Errorf("encode module manifest: %w", err)
		}

		files = append(files, File{Path: path.Join(modulePath, "manifest.json"), Content: content})
	}

	content, err := encodeRecord(version)
	if err != nil {
		return nil, fmt.Errorf("encode version record: %w", err)
	}

	files = append(files, File{
		Path:    path.Join(modulePath, "versions", "v"+version.Version+".json"),
		Content: content,
	})

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	return files, nil
}

func encodeRecord(record any) ([]byte, error) {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(data, '\n'), nil
}

func splitModuleID(id string) (string, string, bool) {
	owner, name, found := strings.Cut(id, "/")

	return owner, name, found && owner != "" && name != "" && !strings.Contains(name, "/")
}
