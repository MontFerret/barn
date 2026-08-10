package barn

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStampVersionsAssignsOneNormalizedTimestamp(t *testing.T) {
	root := t.TempDir()
	writeRegistryRecord(
		t,
		root,
		"montferret",
		"archive",
		testRegistryManifest("montferret", "archive"),
		testVersion("1.0.0", "v1.0.0", testCommit),
		testVersion("1.1.0", "v1.1.0", testCommit),
	)

	if err := CheckVersionsStamped(root); !errors.Is(err, ErrUnstampedVersions) {
		t.Fatalf("expected unstamped registry error, got %v", err)
	}

	input := time.Date(2026, time.August, 7, 17, 54, 12, 987654321, time.FixedZone("EDT", -4*60*60))
	count, err := StampVersions(root, input)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("stamped %d versions, want 2", count)
	}

	registry, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, time.August, 7, 21, 54, 12, 0, time.UTC)
	for _, version := range registry.Modules[0].Versions {
		if version.Record.PublishedAt == nil || !version.Record.PublishedAt.Equal(want) || version.Record.PublishedAt.Location() != time.UTC {
			t.Fatalf("unexpected publication timestamp for %s: %v", version.Record.Version, version.Record.PublishedAt)
		}
	}
	if err := CheckVersionsStamped(root); err != nil {
		t.Fatal(err)
	}
}

func TestStampVersionsPreservesExistingRecordsAndRerunsAsNoOp(t *testing.T) {
	root := t.TempDir()
	existingTimestamp := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	existing := testVersion("1.0.0", "v1.0.0", testCommit)
	existing.PublishedAt = &existingTimestamp
	writeRegistryRecord(
		t,
		root,
		"montferret",
		"archive",
		testRegistryManifest("montferret", "archive"),
		existing,
		testVersion("1.1.0", "v1.1.0", testCommit),
	)

	existingPath := filepath.Join(root, filepath.FromSlash(moduleRegistryPath), "montferret", "archive", "versions", "v1.0.0.json")
	existingBefore, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatal(err)
	}

	stampTime := time.Date(2026, time.August, 7, 21, 54, 12, 0, time.UTC)
	count, err := StampVersions(root, stampTime)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("stamped %d versions, want 1", count)
	}

	existingAfter, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(existingAfter, existingBefore) {
		t.Fatalf("existing stamped record changed:\n%s", existingAfter)
	}

	versionDirectory := filepath.Dir(existingPath)
	newPath := filepath.Join(versionDirectory, "v1.1.0.json")
	newBefore, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err = StampVersions(root, stampTime.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rerun stamped %d versions, want 0", count)
	}
	newAfter, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(newAfter, newBefore) {
		t.Fatalf("rerun changed stamped record:\n%s", newAfter)
	}
}

func TestStampVersionsRejectsZeroTimestamp(t *testing.T) {
	root := t.TempDir()
	writeRegistryRecord(t, root, "montferret", "archive", testRegistryManifest("montferret", "archive"), testVersion("1.0.0", "v1.0.0", testCommit))
	if _, err := StampVersions(root, time.Time{}); err == nil {
		t.Fatal("expected zero timestamp to be rejected")
	}
}

func TestCanonicalRegistryContainsCleanRestartBaseline(t *testing.T) {
	registry, err := Load(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	baseline := map[string]struct{}{
		"montferret/archive@1.0.0-rc.4":   {},
		"montferret/article@1.0.0-rc.16":  {},
		"montferret/csv@1.0.0-rc.17":      {},
		"montferret/html@1.0.0-rc.22":     {},
		"montferret/jwt@1.0.0-rc.13":      {},
		"montferret/llm@1.0.0-rc.5":       {},
		"montferret/oauth2@1.0.0-rc.4":    {},
		"montferret/pdf@1.0.0-rc.9":       {},
		"montferret/postgres@1.0.0-rc.10": {},
		"montferret/redis@1.0.0-rc.3":     {},
		"montferret/rest@1.0.0-rc.12":     {},
		"montferret/robots@1.0.0-rc.15":   {},
		"montferret/sitemap@1.0.0-rc.15":  {},
		"montferret/sqlite@1.0.0-rc.13":   {},
		"montferret/toml@1.0.0-rc.15":     {},
		"montferret/xlsx@1.0.0-rc.9":      {},
		"montferret/xml@1.0.0-rc.15":      {},
		"montferret/yaml@1.0.0-rc.15":     {},
	}
	retired := map[string]struct{}{
		"montferret/archive@1.0.0-rc.3":  {},
		"montferret/article@1.0.0-rc.15": {},
		"montferret/csv@1.0.0-rc.16":     {},
		"montferret/html@1.0.0-rc.21":    {},
		"montferret/jwt@1.0.0-rc.12":     {},
		"montferret/llm@1.0.0-rc.4":      {},
		"montferret/oauth2@1.0.0-rc.3":   {},
		"montferret/pdf@1.0.0-rc.8":      {},
		"montferret/postgres@1.0.0-rc.9": {},
		"montferret/redis@1.0.0-rc.1":    {},
		"montferret/rest@1.0.0-rc.11":    {},
		"montferret/robots@1.0.0-rc.14":  {},
		"montferret/sitemap@1.0.0-rc.14": {},
		"montferret/sqlite@1.0.0-rc.12":  {},
		"montferret/toml@1.0.0-rc.14":    {},
		"montferret/xlsx@1.0.0-rc.8":     {},
		"montferret/xml@1.0.0-rc.14":     {},
		"montferret/yaml@1.0.0-rc.14":    {},
	}

	for _, module := range registry.Modules {
		for _, version := range module.Versions {
			key := module.ID() + "@" + version.Record.Version
			if _, exists := retired[key]; exists {
				t.Fatalf("retired version %s remains in the canonical registry", key)
			}

			delete(baseline, key)
		}
	}

	if len(baseline) != 0 {
		t.Fatalf("clean-restart baseline versions are missing from the canonical registry: %v", baseline)
	}
}
