package barn

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	modulemanifest "github.com/MontFerret/specs/pkg/module"
	registryspec "github.com/MontFerret/specs/pkg/registry"
)

type (
	// Inspector resolves pinned module manifests from registry Git sources.
	Inspector interface {
		Resolve(context.Context, *Registry) error
	}

	// RepositoryResolver maps a validated registry URL to the URL used by Git.
	// Production leaves this nil; tests use it to route fixture HTTPS URLs locally.
	RepositoryResolver func(context.Context, string) (string, error)

	// GitInspector inspects release refs and blobs using the Git executable.
	GitInspector struct {
		Resolver RepositoryResolver
		Timeout  time.Duration
	}

	// ResolvedRelease contains the authoritative content at one resolved Git tag.
	ResolvedRelease struct {
		Commit        string
		Manifest      *modulemanifest.Manifest
		Documentation []byte
	}

	sourceRepository struct {
		directory string
		local     bool
	}
)

const moduleDocumentationFilename = "README.md"

// Resolve validates remote release identities and attaches authoritative manifests.
func (inspector GitInspector) Resolve(ctx context.Context, registry *Registry) error {
	temporaryRoot, err := os.MkdirTemp("", "barn-git-")
	if err != nil {
		return fmt.Errorf("create temporary Git root: %w", err)
	}

	defer os.RemoveAll(temporaryRoot)

	repositories := make(map[registryspec.Source]sourceRepository)

	for _, registryModule := range registry.Modules {
		source := registryModule.Manifest.Source
		repository, exists := repositories[source]

		if !exists {
			resolvedURL, local, err := inspector.resolveRepository(ctx, source.Repository)
			if err != nil {
				return fmt.Errorf("resolve repository for %s: %w", registryModule.ID(), err)
			}

			directory := filepath.Join(temporaryRoot, fmt.Sprintf("repository-%d.git", len(repositories)))
			if _, err := runGit(ctx, "", local, temporaryRoot, "init", "--bare", directory); err != nil {
				return err
			}

			if _, err := runGit(ctx, directory, local, temporaryRoot, "remote", "add", "origin", resolvedURL); err != nil {
				return err
			}

			repository = sourceRepository{directory: directory, local: local}
			repositories[source] = repository
		}

		for _, version := range registryModule.Versions {
			operationContext, cancel := context.WithTimeout(ctx, inspector.timeout())
			release, err := inspectRelease(
				operationContext,
				repository.directory,
				repository.local,
				temporaryRoot,
				registryModule.Manifest.Source,
				registryModule.ID(),
				version.Record.Version,
				version.Record.Tag,
				version.Record.Commit,
			)

			cancel()

			if err != nil {
				return err
			}

			version.Manifest = release.Manifest
			version.Documentation = append([]byte{}, release.Documentation...)
		}
	}

	return nil
}

// Inspect resolves one release tag and returns its authoritative pinned content.
func (inspector GitInspector) Inspect(ctx context.Context, source registryspec.Source, moduleID, version, tag string) (*ResolvedRelease, error) {
	temporaryRoot, err := os.MkdirTemp("", "barn-git-")
	if err != nil {
		return nil, fmt.Errorf("create temporary Git root: %w", err)
	}
	defer os.RemoveAll(temporaryRoot)

	resolvedURL, local, err := inspector.resolveRepository(ctx, source.Repository)
	if err != nil {
		return nil, fmt.Errorf("resolve repository for %s: %w", moduleID, err)
	}

	directory := filepath.Join(temporaryRoot, "repository.git")
	if _, err := runGit(ctx, "", local, temporaryRoot, "init", "--bare", directory); err != nil {
		return nil, err
	}

	if _, err := runGit(ctx, directory, local, temporaryRoot, "remote", "add", "origin", resolvedURL); err != nil {
		return nil, err
	}

	operationContext, cancel := context.WithTimeout(ctx, inspector.timeout())
	defer cancel()

	return inspectRelease(operationContext, directory, local, temporaryRoot, source, moduleID, version, tag, "")
}

func (inspector GitInspector) timeout() time.Duration {
	if inspector.Timeout == 0 {
		return 2 * time.Minute
	}

	return inspector.Timeout
}

func (inspector GitInspector) resolveRepository(ctx context.Context, repository string) (string, bool, error) {
	if inspector.Resolver != nil {
		resolved, err := inspector.Resolver(ctx, repository)

		return resolved, true, err
	}

	if err := requirePublicRepository(ctx, repository); err != nil {
		return "", false, err
	}

	return repository, false, nil
}

func inspectRelease(ctx context.Context, directory string, local bool, temporaryRoot string, source registryspec.Source, moduleID, version, tag, expectedCommit string) (*ResolvedRelease, error) {
	tagRef := "refs/tags/" + tag
	refspec := "+" + tagRef + ":" + tagRef

	if _, err := runGit(ctx, directory, local, temporaryRoot, "fetch", "--force", "--no-tags", "--depth=1", "origin", refspec); err != nil {
		return nil, fmt.Errorf("fetch tag %q for %s@%s: %w", tag, moduleID, version, err)
	}

	resolved, err := runGit(ctx, directory, local, temporaryRoot, "rev-parse", "--verify", tagRef+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve tag %q for %s@%s: %w", tag, moduleID, version, err)
	}

	commit := strings.TrimSpace(string(resolved))
	if expectedCommit != "" && commit != expectedCommit {
		return nil, fmt.Errorf("tag %q for %s@%s resolves to %s, not declared commit %s", tag, moduleID, version, commit, expectedCommit)
	}

	if _, err := runGit(ctx, directory, local, temporaryRoot, "cat-file", "-e", commit+"^{commit}"); err != nil {
		return nil, fmt.Errorf("resolved commit %s for %s@%s does not exist: %w", commit, moduleID, version, err)
	}

	manifestPath := modulemanifest.ManifestFilename
	documentationPath := moduleDocumentationFilename
	if source.Path != "" {
		objectType, err := runGit(ctx, directory, local, temporaryRoot, "cat-file", "-t", commit+":"+source.Path)
		if err != nil {
			return nil, fmt.Errorf("source path %s does not exist at %s for %s@%s: %w", source.Path, commit, moduleID, version, err)
		}

		if strings.TrimSpace(string(objectType)) != "tree" {
			return nil, fmt.Errorf("source path %s is not a directory at %s for %s@%s", source.Path, commit, moduleID, version)
		}

		manifestPath = path.Join(source.Path, modulemanifest.ManifestFilename)
		documentationPath = path.Join(source.Path, moduleDocumentationFilename)
	}

	data, err := runGit(ctx, directory, local, temporaryRoot, "show", commit+":"+manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read %s at %s for %s@%s: %w", manifestPath, commit, moduleID, version, err)
	}

	manifest, err := modulemanifest.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("validate %s at %s for %s@%s: %w", manifestPath, commit, moduleID, version, err)
	}

	if manifest.Name != moduleID {
		return nil, fmt.Errorf("source manifest name %q does not match registry module %q", manifest.Name, moduleID)
	}

	if manifest.Version != version {
		return nil, fmt.Errorf("source manifest version %q does not match registry version %q", manifest.Version, version)
	}
	if err := validateCategoryIDs(moduleID, version, manifest.Categories); err != nil {
		return nil, err
	}

	documentation, err := runGit(ctx, directory, local, temporaryRoot, "show", commit+":"+documentationPath)
	if err != nil {
		return nil, fmt.Errorf("read %s at %s for %s@%s: %w", documentationPath, commit, moduleID, version, err)
	}

	return &ResolvedRelease{
		Commit:        commit,
		Manifest:      manifest,
		Documentation: append([]byte{}, documentation...),
	}, nil
}

func requirePublicRepository(ctx context.Context, repository string) error {
	parsed, err := url.Parse(repository)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return fmt.Errorf("repository must be an HTTPS URL")
	}

	hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return fmt.Errorf("repository host %q is not public", parsed.Hostname())
	}

	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", parsed.Hostname())
	if err != nil {
		return fmt.Errorf("resolve repository host %q: %w", parsed.Hostname(), err)
	}

	if len(addresses) == 0 {
		return fmt.Errorf("repository host %q has no addresses", parsed.Hostname())
	}

	for _, address := range addresses {
		if !isPublicAddress(address) {
			return fmt.Errorf("repository host %q resolves to non-public address %s", parsed.Hostname(), address)
		}
	}

	return nil
}

func isPublicAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() ||
		address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		address.IsUnspecified() || address.IsMulticast() {

		return false
	}

	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address.Unmap()) {
			return false
		}
	}

	return true
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func runGit(ctx context.Context, directory string, allowFile bool, temporaryRoot string, arguments ...string) ([]byte, error) {
	config := []string{
		"-c", "credential.helper=",
		"-c", "protocol.allow=never",
		"-c", "protocol.https.allow=always",
		"-c", "http.followRedirects=false",
	}

	if allowFile {
		config = append(config, "-c", "protocol.file.allow=always")
	}

	command := exec.CommandContext(ctx, "git", append(config, arguments...)...)
	command.Dir = directory
	command.Env = append(cleanGitEnvironment(os.Environ()),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=/usr/bin/false",
		"SSH_ASKPASS=/usr/bin/false",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"HOME="+temporaryRoot,
	)

	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()

	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}

		return nil, fmt.Errorf("git %s: %s", strings.Join(arguments, " "), message)
	}

	return output, nil
}

func cleanGitEnvironment(environment []string) []string {
	cleaned := make([]string, 0, len(environment))

	for _, value := range environment {
		name, _, _ := strings.Cut(value, "=")
		upperName := strings.ToUpper(name)

		if name == "HOME" || name == "SSH_ASKPASS" || strings.HasPrefix(name, "GIT_") ||
			upperName == "HTTP_PROXY" || upperName == "HTTPS_PROXY" || upperName == "ALL_PROXY" || upperName == "NO_PROXY" {

			continue
		}

		cleaned = append(cleaned, value)
	}

	return cleaned
}
