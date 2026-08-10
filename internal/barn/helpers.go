package barn

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/MontFerret/barn/internal/barn/apiref"
	"github.com/MontFerret/specs/pkg/api"
	modulemanifest "github.com/MontFerret/specs/pkg/module"
	registryspec "github.com/MontFerret/specs/pkg/registry"
)

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

func runGit(ctx context.Context, directory string, allowFile bool, temporaryRoot string, arguments ...string) ([]byte, error) {
	return runGitInput(ctx, directory, allowFile, temporaryRoot, nil, arguments...)
}

func runGitInput(ctx context.Context, directory string, allowFile bool, temporaryRoot string, input []byte, arguments ...string) ([]byte, error) {
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
	command.Stdin = bytes.NewReader(input)
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

func inspectRelease(ctx context.Context, directory string, local bool, temporaryRoot string, analyzer APIAnalyzer, source registryspec.Source, moduleID, version, tag, expectedCommit string) (*ResolvedRelease, error) {
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
	packagePath := modulePackageFilename
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
		packagePath = path.Join(source.Path, modulePackageFilename)
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

	packageData, err := runGit(ctx, directory, local, temporaryRoot, "show", commit+":"+packagePath)
	if err != nil {
		return nil, fmt.Errorf("read %s at %s for %s@%s: %w", packagePath, commit, moduleID, version, err)
	}

	resolvedPackagePath, err := parseModulePackage(packagePath, packageData, version)
	if err != nil {
		return nil, fmt.Errorf("validate %s at %s for %s@%s: %w", packagePath, commit, moduleID, version, err)
	}

	documentation, err := runGit(ctx, directory, local, temporaryRoot, "show", commit+":"+documentationPath)
	if err != nil {
		return nil, fmt.Errorf("read %s at %s for %s@%s: %w", documentationPath, commit, moduleID, version, err)
	}

	materializedRoot := filepath.Join(temporaryRoot, fmt.Sprintf("source-%x", sha256.Sum256([]byte(source.Repository+"\x00"+commit))))
	if err := materializeCommit(ctx, directory, local, temporaryRoot, commit, materializedRoot); err != nil {
		return nil, fmt.Errorf("materialize commit %s for %s@%s: %w", commit, moduleID, version, err)
	}

	moduleDirectory := materializedRoot
	if source.Path != "" {
		moduleDirectory = filepath.Join(materializedRoot, filepath.FromSlash(source.Path))
	}

	if err := validateLocalReplacements(packagePath, packageData, materializedRoot, moduleDirectory); err != nil {
		return nil, &apiref.AnalysisError{
			Kind:     apiref.ErrorInvalidPackage,
			ModuleID: moduleID,
			Version:  version,
			Err:      fmt.Errorf("validate %s at %s: %w", packagePath, commit, err),
		}
	}

	reference, err := analyzer.Analyze(ctx, materializedRoot, moduleDirectory, resolvedPackagePath, moduleID, version)
	if err != nil {
		return nil, err
	}

	if reference == nil || reference.ID != moduleID || reference.Version != version {
		return nil, &apiref.AnalysisError{
			Kind:     apiref.ErrorInternal,
			ModuleID: moduleID,
			Version:  version,
			Err:      fmt.Errorf("analyzer returned an absent or mismatched API Reference"),
		}
	}

	if err := api.Validate(reference); err != nil {
		return nil, &apiref.AnalysisError{
			Kind:     apiref.ErrorInternal,
			ModuleID: moduleID,
			Version:  version,
			Err:      fmt.Errorf("analyzer returned an invalid API Reference: %w", err),
		}
	}

	return &ResolvedRelease{
		Commit:        commit,
		Manifest:      manifest,
		PackagePath:   resolvedPackagePath,
		Documentation: append([]byte{}, documentation...),
		API:           reference,
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
