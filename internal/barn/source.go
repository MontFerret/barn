package barn

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/mod/modfile"
)

type sourceTreeEntry struct {
	path   string
	object string
	mode   os.FileMode
}

func materializeCommit(ctx context.Context, repository string, local bool, temporaryRoot, commit, destination string) error {
	if information, err := os.Stat(destination); err == nil && information.IsDir() {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	tree, err := runGit(ctx, repository, local, temporaryRoot, "ls-tree", "-rz", "--full-tree", commit)
	if err != nil {
		return err
	}

	entries, err := parseSourceTree(tree)
	if err != nil {
		return err
	}

	var objectInput strings.Builder

	for _, entry := range entries {
		objectInput.WriteString(entry.object)
		objectInput.WriteByte('\n')
	}

	blobs, err := runGitInput(ctx, repository, local, temporaryRoot, []byte(objectInput.String()), "cat-file", "--batch")
	if err != nil {
		return err
	}

	staging, err := os.MkdirTemp(temporaryRoot, "extract-")
	if err != nil {
		return fmt.Errorf("create extraction directory: %w", err)
	}

	defer os.RemoveAll(staging)

	reader := bufio.NewReader(bytes.NewReader(blobs))

	for _, entry := range entries {
		data, err := readBatchBlob(reader, entry.object)
		if err != nil {
			return fmt.Errorf("read Git blob for %q: %w", entry.path, err)
		}

		target := filepath.Join(staging, filepath.FromSlash(entry.path))
		if !sourcePathWithin(staging, target) {
			return fmt.Errorf("source entry %q escapes the repository", entry.path)
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create source parent for %q: %w", entry.path, err)
		}

		if err := os.WriteFile(target, data, entry.mode); err != nil {
			return fmt.Errorf("extract source file %q: %w", entry.path, err)
		}
	}

	if _, err := reader.ReadByte(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("git blob batch contains trailing data")
		}

		return fmt.Errorf("read Git blob batch trailer: %w", err)
	}

	if err := os.Rename(staging, destination); err != nil {
		if information, statErr := os.Stat(destination); statErr == nil && information.IsDir() {
			return nil
		}

		return fmt.Errorf("publish extracted tree: %w", err)
	}

	return nil
}

func parseSourceTree(data []byte) ([]sourceTreeEntry, error) {
	records := bytes.Split(data, []byte{0})
	entries := make([]sourceTreeEntry, 0, len(records)-1)
	seen := make(map[string]struct{}, len(records)-1)

	for _, record := range records {
		if len(record) == 0 {
			continue
		}

		metadata, name, found := bytes.Cut(record, []byte{'\t'})
		if !found {
			return nil, fmt.Errorf("malformed Git tree entry")
		}

		fields := bytes.Fields(metadata)
		if len(fields) != 3 {
			return nil, fmt.Errorf("malformed Git tree metadata %q", metadata)
		}

		entryPath := string(name)

		if !utf8.Valid(name) || !validSourceEntryPath(entryPath) {
			return nil, fmt.Errorf("source entry %q escapes the repository", entryPath)
		}

		if _, exists := seen[entryPath]; exists {
			return nil, fmt.Errorf("source entry %q occurs more than once", entryPath)
		}

		seen[entryPath] = struct{}{}

		mode := os.FileMode(0)

		switch string(fields[0]) {
		case "100644":
			mode = 0o644
		case "100755":
			mode = 0o755
		default:
			return nil, fmt.Errorf("source entry %q has unsupported Git mode %s", entryPath, fields[0])
		}

		if string(fields[1]) != "blob" {
			return nil, fmt.Errorf("source entry %q has unsupported Git type %s", entryPath, fields[1])
		}

		entries = append(entries, sourceTreeEntry{path: entryPath, object: string(fields[2]), mode: mode})
	}

	return entries, nil
}

func validSourceEntryPath(value string) bool {
	cleaned := path.Clean(value)

	return cleaned != "." && cleaned != "" && !path.IsAbs(cleaned) && cleaned != ".." &&
		!strings.HasPrefix(cleaned, "../") && !strings.Contains(cleaned, `\`) && cleaned == value
}

func readBatchBlob(reader *bufio.Reader, expectedObject string) ([]byte, error) {
	header, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(strings.TrimSuffix(header, "\n"))
	if len(fields) != 3 || fields[0] != expectedObject || fields[1] != "blob" {
		return nil, fmt.Errorf("unexpected Git batch header %q", strings.TrimSpace(header))
	}

	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 || size > int64(^uint(0)>>1) {
		return nil, fmt.Errorf("invalid Git blob size %q", fields[2])
	}

	data := make([]byte, int(size))
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}

	delimiter, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}

	if delimiter != '\n' {
		return nil, fmt.Errorf("git blob is not newline-delimited")
	}

	return data, nil
}

func validateLocalReplacements(filename string, data []byte, repositoryRoot, moduleDirectory string) error {
	file, err := modfile.Parse(filename, data, nil)
	if err != nil {
		return err
	}

	for _, replacement := range file.Replace {
		if replacement.New.Version != "" {
			continue
		}

		if replacement.New.Path == "" || filepath.IsAbs(replacement.New.Path) {
			return fmt.Errorf("local replacement for %s escapes the extracted repository", replacement.Old.Path)
		}

		resolved := filepath.Clean(filepath.Join(moduleDirectory, filepath.FromSlash(replacement.New.Path)))
		if !sourcePathWithin(repositoryRoot, resolved) {
			return fmt.Errorf("local replacement for %s escapes the extracted repository: %s", replacement.Old.Path, replacement.New.Path)
		}

		information, err := os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("local replacement for %s is unavailable: %w", replacement.Old.Path, err)
		}

		if !information.IsDir() {
			return fmt.Errorf("local replacement for %s is not a directory", replacement.Old.Path)
		}
	}

	return nil
}

func sourcePathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)

	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
