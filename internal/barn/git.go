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

	sourceRepository struct {
		directory string
		local     bool
	}
)

// Resolve validates remote release identities and attaches authoritative manifests.
func (inspector GitInspector) Resolve(ctx context.Context, registry *Registry) error {
	temporaryRoot, err := os.MkdirTemp("", "barn-git-")
	if err != nil {
		return fmt.Errorf("create temporary Git root: %w", err)
	}

	defer os.RemoveAll(temporaryRoot)

	timeout := inspector.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}

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
			operationContext, cancel := context.WithTimeout(ctx, timeout)
			err := inspectVersion(operationContext, repository.directory, repository.local, temporaryRoot, registryModule, version)

			cancel()

			if err != nil {
				return err
			}
		}
	}

	return nil
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

func inspectVersion(ctx context.Context, directory string, local bool, temporaryRoot string, registryModule *Module, version *Version) error {
	tagRef := "refs/tags/" + version.Record.Tag
	refspec := "+" + tagRef + ":" + tagRef

	if _, err := runGit(ctx, directory, local, temporaryRoot, "fetch", "--force", "--no-tags", "--depth=1", "origin", refspec); err != nil {
		return fmt.Errorf("fetch tag %q for %s@%s: %w", version.Record.Tag, registryModule.ID(), version.Record.Version, err)
	}

	resolved, err := runGit(ctx, directory, local, temporaryRoot, "rev-parse", "--verify", tagRef+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolve tag %q for %s@%s: %w", version.Record.Tag, registryModule.ID(), version.Record.Version, err)
	}

	commit := strings.TrimSpace(string(resolved))
	if commit != version.Record.Commit {
		return fmt.Errorf("tag %q for %s@%s resolves to %s, not declared commit %s", version.Record.Tag, registryModule.ID(), version.Record.Version, commit, version.Record.Commit)
	}

	if _, err := runGit(ctx, directory, local, temporaryRoot, "cat-file", "-e", version.Record.Commit+"^{commit}"); err != nil {
		return fmt.Errorf("declared commit %s for %s@%s does not exist: %w", version.Record.Commit, registryModule.ID(), version.Record.Version, err)
	}

	manifestPath := moduleManifestFilename
	if registryModule.Manifest.Source.Path != "" {
		objectType, err := runGit(ctx, directory, local, temporaryRoot, "cat-file", "-t", version.Record.Commit+":"+registryModule.Manifest.Source.Path)
		if err != nil {
			return fmt.Errorf("source path %s does not exist at %s for %s@%s: %w", registryModule.Manifest.Source.Path, version.Record.Commit, registryModule.ID(), version.Record.Version, err)
		}

		if strings.TrimSpace(string(objectType)) != "tree" {
			return fmt.Errorf("source path %s is not a directory at %s for %s@%s", registryModule.Manifest.Source.Path, version.Record.Commit, registryModule.ID(), version.Record.Version)
		}

		manifestPath = path.Join(registryModule.Manifest.Source.Path, moduleManifestFilename)
	}

	data, err := runGit(ctx, directory, local, temporaryRoot, "show", version.Record.Commit+":"+manifestPath)
	if err != nil {
		return fmt.Errorf("read %s at %s for %s@%s: %w", manifestPath, version.Record.Commit, registryModule.ID(), version.Record.Version, err)
	}

	manifest, err := modulemanifest.Parse(data)
	if err != nil {
		return fmt.Errorf("validate %s at %s for %s@%s: %w", manifestPath, version.Record.Commit, registryModule.ID(), version.Record.Version, err)
	}

	if manifest.Name != registryModule.ID() {
		return fmt.Errorf("source manifest name %q does not match registry module %q", manifest.Name, registryModule.ID())
	}

	if manifest.Version != version.Record.Version {
		return fmt.Errorf("source manifest version %q does not match registry version %q", manifest.Version, version.Record.Version)
	}

	version.Manifest = manifest

	return nil
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
