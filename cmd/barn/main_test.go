package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	registryartifact "github.com/MontFerret/specs/pkg/registry/artifact"
)

func TestGenerateDefaultsToFullHeadDistribution(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"registry/modules", "registry/plugins"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directory)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "registry", "modules", ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "registry", "plugins", ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	runCommand(t, root, "git", "init")
	runCommand(t, root, "git", "config", "user.name", "Barn Tests")
	runCommand(t, root, "git", "config", "user.email", "barn-tests@example.org")
	runCommand(t, root, "git", "add", ".")
	runCommand(t, root, "git", "commit", "-m", "initial registry")
	commit := strings.TrimSpace(runCommand(t, root, "git", "rev-parse", "HEAD"))

	if err := run(context.Background(), []string{"generate", "--root", root}); err != nil {
		t.Fatal(err)
	}

	rootData, err := os.ReadFile(filepath.Join(root, "dist", "index.json"))
	if err != nil {
		t.Fatal(err)
	}

	rootIndex, err := registryartifact.ParseRootIndex(rootData)
	if err != nil {
		t.Fatal(err)
	}

	if rootIndex.Source.Commit != commit {
		t.Fatalf("source commit is %s, want %s", rootIndex.Source.Commit, commit)
	}

	if err := run(context.Background(), []string{"verify-tree", "--root", root, "--output", filepath.Join(root, "dist")}); err != nil {
		t.Fatal(err)
	}
}

func runCommand(t *testing.T, directory, name string, arguments ...string) string {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v: %s", name, strings.Join(arguments, " "), err, output)
	}

	return string(output)
}
