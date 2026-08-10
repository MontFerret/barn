package barn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeCommitUsesExactCommit(t *testing.T) {
	repository := t.TempDir()
	runTestGit(t, repository, "init")
	runTestGit(t, repository, "config", "user.name", "Barn Tests")
	runTestGit(t, repository, "config", "user.email", "barn-tests@example.org")
	filename := filepath.Join(repository, "source.go")
	if err := os.WriteFile(filename, []byte("pinned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repository, "add", "source.go")
	runTestGit(t, repository, "commit", "-m", "pinned")
	commit := strings.TrimSpace(runTestGit(t, repository, "rev-parse", "HEAD"))
	if err := os.WriteFile(filename, []byte("working tree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	temporaryRoot := t.TempDir()
	destination := filepath.Join(temporaryRoot, "source")
	if err := materializeCommit(context.Background(), repository, true, temporaryRoot, commit, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "source.go"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "pinned\n" {
		t.Fatalf("materialized data = %q", got)
	}
}

func TestMaterializeCommitIgnoresArchiveAttributes(t *testing.T) {
	repository := t.TempDir()
	runTestGit(t, repository, "init")
	runTestGit(t, repository, "config", "user.name", "Barn Tests")
	runTestGit(t, repository, "config", "user.email", "barn-tests@example.org")
	files := map[string]string{
		".gitattributes":  "hidden.go export-ignore\nsubstituted.txt export-subst\n",
		"hidden.go":       "package hidden\n",
		"substituted.txt": "$Format:%H$\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(repository, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runTestGit(t, repository, "add", ".gitattributes", "hidden.go", "substituted.txt")
	runTestGit(t, repository, "commit", "-m", "archive attributes")
	commit := strings.TrimSpace(runTestGit(t, repository, "rev-parse", "HEAD"))
	destination := filepath.Join(t.TempDir(), "source")

	if err := materializeCommit(context.Background(), repository, true, filepath.Dir(destination), commit, destination); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"hidden.go": "package hidden\n", "substituted.txt": "$Format:%H$\n"} {
		data, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want %q", name, data, want)
		}
	}
}

func TestMaterializeCommitRejectsSymlinks(t *testing.T) {
	repository := t.TempDir()
	runTestGit(t, repository, "init")
	runTestGit(t, repository, "config", "user.name", "Barn Tests")
	runTestGit(t, repository, "config", "user.email", "barn-tests@example.org")
	if err := os.WriteFile(filepath.Join(repository, "target"), []byte("safe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(repository, "link")); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repository, "add", "target", "link")
	runTestGit(t, repository, "commit", "-m", "symlink")
	commit := strings.TrimSpace(runTestGit(t, repository, "rev-parse", "HEAD"))
	temporaryRoot := t.TempDir()

	err := materializeCommit(context.Background(), repository, true, temporaryRoot, commit, filepath.Join(temporaryRoot, "source"))
	if err == nil || !strings.Contains(err.Error(), "unsupported Git mode 120000") {
		t.Fatalf("expected symlink archive rejection, got %v", err)
	}
}

func TestParseSourceTreeRejectsUnsafeEntries(t *testing.T) {
	object := strings.Repeat("a", 40)
	for _, test := range []struct {
		name      string
		entry     string
		wantError string
	}{
		{name: "parent path", entry: "100644 blob " + object + "\t../escape\x00", wantError: "escapes the repository"},
		{name: "gitlink", entry: "160000 commit " + object + "\tdependency\x00", wantError: "unsupported Git mode 160000"},
		{name: "tree", entry: "040000 tree " + object + "\tdirectory\x00", wantError: "unsupported Git mode 040000"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseSourceTree([]byte(test.entry))
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestValidateLocalReplacements(t *testing.T) {
	repository := t.TempDir()
	moduleDirectory := filepath.Join(repository, "modules", "fixture")
	sharedDirectory := filepath.Join(repository, "shared")
	for _, directory := range []string{moduleDirectory, sharedDirectory} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := validateLocalReplacements("go.mod", []byte("module example.com/fixture\n\nreplace example.com/shared => ../../shared\n"), repository, moduleDirectory); err != nil {
		t.Fatalf("safe replacement rejected: %v", err)
	}
	for _, replacement := range []string{"../../../outside", "/private/tmp/outside", "../../missing"} {
		data := []byte("module example.com/fixture\n\nreplace example.com/shared => " + replacement + "\n")
		if err := validateLocalReplacements("go.mod", data, repository, moduleDirectory); err == nil {
			t.Fatalf("replacement %q was accepted", replacement)
		}
	}
}
