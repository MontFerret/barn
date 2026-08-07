package barn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	modulemanifest "github.com/MontFerret/specs/pkg/module"
	registryspec "github.com/MontFerret/specs/pkg/registry"
)

const (
	testCommit        = "0123456789abcdef0123456789abcdef01234567"
	testDocumentation = "# Fixture documentation\n"
)

func writeRegistryRecord(t *testing.T, root, ownerDirectory, moduleDirectory string, manifest *registryspec.ModuleManifest, records ...*registryspec.VersionRecord) {
	t.Helper()
	ensureRegistryRoots(t, root)
	directory := filepath.Join(root, filepath.FromSlash(moduleRegistryPath), ownerDirectory, moduleDirectory)
	writeRegistryRecordInDirectory(t, directory, manifest, records...)
}

func writeLegacyRegistryRecord(t *testing.T, root, ownerDirectory, moduleDirectory string, manifest *registryspec.ModuleManifest, records ...*registryspec.VersionRecord) {
	t.Helper()
	directory := filepath.Join(root, "modules", ownerDirectory, moduleDirectory)
	writeRegistryRecordInDirectory(t, directory, manifest, records...)
}

func writeRegistryRecordInDirectory(t *testing.T, directory string, manifest *registryspec.ModuleManifest, records ...*registryspec.VersionRecord) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(directory, "versions"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(directory, "manifest.json"), manifest)
	for _, record := range records {
		writeJSON(t, filepath.Join(directory, "versions", "v"+record.Version+".json"), record)
	}
}

func ensureRegistryRoots(t *testing.T, root string) {
	t.Helper()
	for _, relative := range []string{moduleRegistryPath, pluginRegistryPath} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(relative)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func writeJSON(t *testing.T, filePath string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func testRegistryManifest(owner, name string) *registryspec.ModuleManifest {
	return &registryspec.ModuleManifest{
		Schema: registryspec.ModuleManifestSchemaV1,
		Owner:  owner,
		Name:   name,
		Source: registryspec.Source{
			Repository: "https://fixtures.invalid/source.git",
		},
	}
}

func testVersion(version, tag, commit string) *registryspec.VersionRecord {
	return &registryspec.VersionRecord{
		Schema:  registryspec.VersionRecordSchemaV1,
		Version: version,
		Tag:     tag,
		Commit:  commit,
	}
}

func testModuleManifest(name, namespace, version, description string) *modulemanifest.Manifest {
	return &modulemanifest.Manifest{
		Schema:        modulemanifest.SchemaV1,
		Name:          name,
		Namespace:     namespace,
		Version:       version,
		Description:   description,
		License:       "Apache-2.0",
		Documentation: "https://docs.example.org/modules/" + strings.ReplaceAll(name, "/", "-") + "/",
		Repository:    &modulemanifest.Repository{URL: "https://fixtures.invalid/source.git"},
	}
}

type gitFixture struct {
	directory string
	commit    string
}

func newGitFixture(t *testing.T, sourcePath string, manifest *modulemanifest.Manifest, tag string, annotated bool) gitFixture {
	t.Helper()

	return newGitFixtureWithDocumentation(t, sourcePath, manifest, tag, annotated, []byte(testDocumentation))
}

func newGitFixtureWithDocumentation(t *testing.T, sourcePath string, manifest *modulemanifest.Manifest, tag string, annotated bool, documentation []byte) gitFixture {
	t.Helper()

	return newGitFixtureRepository(t, sourcePath, manifest, modulemanifest.ManifestFilename, tag, annotated, documentation)
}

func newGitFixtureWithoutDocumentation(t *testing.T, sourcePath string, manifest *modulemanifest.Manifest, tag string, annotated bool) gitFixture {
	t.Helper()

	return newGitFixtureRepository(t, sourcePath, manifest, modulemanifest.ManifestFilename, tag, annotated, nil)
}

func newGitFixtureWithManifestFilename(t *testing.T, sourcePath string, manifest *modulemanifest.Manifest, manifestFilename, tag string, annotated bool) gitFixture {
	t.Helper()

	return newGitFixtureRepository(t, sourcePath, manifest, manifestFilename, tag, annotated, []byte(testDocumentation))
}

func newGitFixtureRepository(t *testing.T, sourcePath string, manifest *modulemanifest.Manifest, manifestFilename, tag string, annotated bool, documentation []byte) gitFixture {
	t.Helper()
	directory := t.TempDir()
	runTestGit(t, directory, "init")
	runTestGit(t, directory, "config", "user.name", "Barn Tests")
	runTestGit(t, directory, "config", "user.email", "barn-tests@example.org")
	manifestDirectory := directory
	if sourcePath != "" {
		manifestDirectory = filepath.Join(directory, filepath.FromSlash(sourcePath))
		if err := os.MkdirAll(manifestDirectory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if manifest != nil {
		writeModuleYAML(t, filepath.Join(manifestDirectory, manifestFilename), manifest)
	}
	if documentation != nil {
		if err := os.WriteFile(filepath.Join(manifestDirectory, moduleDocumentationFilename), documentation, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runTestGit(t, directory, "add", ".")
	runTestGit(t, directory, "commit", "-m", "fixture release")
	commit := strings.TrimSpace(runTestGit(t, directory, "rev-parse", "HEAD"))
	if tag != "" {
		if annotated {
			runTestGit(t, directory, "tag", "-a", tag, "-m", "fixture tag")
		} else {
			runTestGit(t, directory, "tag", tag)
		}
	}
	return gitFixture{directory: directory, commit: commit}
}

func writeModuleYAML(t *testing.T, filePath string, manifest *modulemanifest.Manifest) {
	t.Helper()
	compatibility := ""
	if manifest.Compatibility != nil {
		compatibility = fmt.Sprintf("compatibility:\n  ferret: %q\n", manifest.Compatibility.Ferret)
	}
	repository := ""
	if manifest.Repository != nil {
		repository = fmt.Sprintf("repository:\n  url: %s\n", manifest.Repository.URL)
		if manifest.Repository.Directory != "" {
			repository += fmt.Sprintf("  directory: %s\n", manifest.Repository.Directory)
		}
	}
	data := fmt.Sprintf("$schema: %s\nname: %s\nnamespace: %s\nversion: %s\ndescription: %q\nlicense: %s\ndocumentation: %s\n%s%s",
		manifest.Schema,
		manifest.Name,
		manifest.Namespace,
		manifest.Version,
		manifest.Description,
		manifest.License,
		manifest.Documentation,
		repository,
		compatibility,
	)
	if err := os.WriteFile(filePath, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runTestGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.CommandContext(context.Background(), "git", arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func fixtureResolver(directory string) RepositoryResolver {
	return func(_ context.Context, repository string) (string, error) {
		if repository != "https://fixtures.invalid/source.git" {
			return "", fmt.Errorf("unexpected fixture repository %s", repository)
		}
		return directory, nil
	}
}
