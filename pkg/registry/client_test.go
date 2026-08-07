package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientReadsAndSearchesStaticDistribution(t *testing.T) {
	server := newDistributionServer(t, "", false)
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	modules, err := client.Modules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantModules := []ModuleSummary{
		{ID: "acme/browser"},
		{ID: "montferret/archive", Latest: "1.0.0"},
	}
	if !reflect.DeepEqual(modules, wantModules) {
		t.Fatalf("unexpected modules: %#v", modules)
	}

	module, err := client.Module(context.Background(), "montferret/archive")
	if err != nil {
		t.Fatal(err)
	}
	if module.ID != "montferret/archive" || module.Owner != "montferret" || module.Name != "archive" || module.Description != "Archive support." || module.Latest != "1.0.0" {
		t.Fatalf("unexpected module: %#v", module)
	}
	wantVersions := []VersionSummary{{Version: "1.0.0"}, {Version: "0.9.0"}}
	if !reflect.DeepEqual(module.Versions, wantVersions) {
		t.Fatalf("unexpected module versions: %#v", module.Versions)
	}

	versions, err := client.Versions(context.Background(), "montferret/archive")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(versions, wantVersions) {
		t.Fatalf("unexpected versions: %#v", versions)
	}

	version, err := client.Version(context.Background(), "montferret/archive", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if version.ID != "montferret/archive" || version.Version != "1.0.0" || version.Namespace != "ARCHIVE" || version.Ferret != ">=2.0.0 <3.0.0" {
		t.Fatalf("unexpected version: %#v", version)
	}
	if version.Source != (Source{Repository: "https://example.com/archive.git", Path: "modules/archive", Commit: strings.Repeat("a", 40)}) {
		t.Fatalf("unexpected source: %#v", version.Source)
	}
	if got := version.Content["documentation"]; got != server.URL+"/records/releases/1.0.0/docs.md" {
		t.Fatalf("unexpected resolved documentation URL: %q", got)
	}

	categories, err := client.Categories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantCategories := []Category{
		{ID: "ai", Name: "AI", Count: 0},
		{ID: "files", Name: "Files", Count: 2},
	}
	if !reflect.DeepEqual(categories, wantCategories) {
		t.Fatalf("unexpected categories: %#v", categories)
	}

	results, err := client.Search(context.Background(), SearchOptions{Query: "ARCH", Category: "files"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(results, []ModuleSummary{{ID: "montferret/archive", Latest: "1.0.0"}}) {
		t.Fatalf("unexpected filtered results: %#v", results)
	}

	results, err = client.Search(context.Background(), SearchOptions{Query: "browser"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(results, []ModuleSummary{{ID: "acme/browser"}}) {
		t.Fatalf("unexpected text results: %#v", results)
	}

	results, err = client.Search(context.Background(), SearchOptions{Query: " AI "})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(results, []ModuleSummary{{ID: "acme/browser"}}) {
		t.Fatalf("unexpected description results: %#v", results)
	}

	results, err = client.Search(context.Background(), SearchOptions{Query: "support", Category: "files"})
	if err != nil {
		t.Fatal(err)
	}
	wantDescriptionResults := []ModuleSummary{
		{ID: "acme/browser"},
		{ID: "montferret/archive", Latest: "1.0.0"},
	}
	if !reflect.DeepEqual(results, wantDescriptionResults) {
		t.Fatalf("unexpected category description results: %#v", results)
	}

	results, err = client.Search(context.Background(), SearchOptions{Category: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if results == nil || len(results) != 0 {
		t.Fatalf("unknown category should return an empty non-nil result: %#v", results)
	}
}

func TestClientSearchAvoidsUnneededDescriptionRequests(t *testing.T) {
	var detailRequests atomic.Int32
	server := newSingleModuleSearchServer(t, func(response http.ResponseWriter, _ *http.Request) {
		detailRequests.Add(1)
		http.Error(response, "unexpected detail request", http.StatusInternalServerError)
	})
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	results, err := client.Search(context.Background(), SearchOptions{Query: "browser"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(results, []ModuleSummary{{ID: "acme/browser"}}) || detailRequests.Load() != 0 {
		t.Fatalf("ID search loaded module details: results=%#v requests=%d", results, detailRequests.Load())
	}

	results, err = client.Search(context.Background(), SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(results, []ModuleSummary{{ID: "acme/browser"}}) || detailRequests.Load() != 0 {
		t.Fatalf("empty search loaded module details: results=%#v requests=%d", results, detailRequests.Load())
	}
}

func TestClientSearchDescriptionRequestErrorsAndCancellation(t *testing.T) {
	t.Run("HTTP error", func(t *testing.T) {
		server := newSingleModuleSearchServer(t, func(response http.ResponseWriter, _ *http.Request) {
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
		})
		defer server.Close()

		client, err := NewClient(WithBaseURL(server.URL))
		if err != nil {
			t.Fatal(err)
		}

		_, err = client.Search(context.Background(), SearchOptions{Query: "needle"})
		var httpError *HTTPError
		if !errors.As(err, &httpError) || httpError.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected typed HTTP error, got %v", err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		started := make(chan struct{}, 1)
		server := newSingleModuleSearchServer(t, func(_ http.ResponseWriter, request *http.Request) {
			started <- struct{}{}
			<-request.Context().Done()
		})
		defer server.Close()

		client, err := NewClient(WithBaseURL(server.URL))
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, searchErr := client.Search(ctx, SearchOptions{Query: "needle"})
			done <- searchErr
		}()

		select {
		case <-started:
			cancel()
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("description request did not start")
		}

		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context cancellation, got %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("canceled search did not return")
		}
	})
}

func TestClientSearchBoundsConcurrentDescriptionRequests(t *testing.T) {
	const candidates = maxSearchConcurrent + 2

	var entries strings.Builder
	documents := make(map[string]string, candidates)
	for index := range candidates {
		if index > 0 {
			entries.WriteByte(',')
		}

		id := fmt.Sprintf("acme/module-%02d", index)
		href := fmt.Sprintf("/records/module-%02d.json", index)
		fmt.Fprintf(&entries, `{"id":%q,"href":%q}`, id, href)
		documents[href] = fmt.Sprintf(`{
  "schemaVersion": 1,
  "id": %q,
  "owner": "acme",
  "name": %q,
  "description": "Needle support.",
  "versions": [{"version":"1.0.0","href":"./1.0.0.json"}]
}`, id, fmt.Sprintf("module-%02d", index))
	}

	started := make(chan struct{}, candidates)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)

	var active atomic.Int32
	var maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/index.json":
			_, _ = response.Write([]byte(`{"schemaVersion":1,"artifacts":{"modules":"/modules.json"}}`))
		case "/modules.json":
			_, _ = fmt.Fprintf(response, `{"schemaVersion":1,"modules":[%s]}`, entries.String())
		default:
			document, exists := documents[request.URL.Path]
			if !exists {
				http.NotFound(response, request)

				return
			}

			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			started <- struct{}{}

			select {
			case <-release:
			case <-request.Context().Done():
			}
			active.Add(-1)
			_, _ = response.Write([]byte(document))
		}
	}))
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	type searchResult struct {
		modules []ModuleSummary
		err     error
	}
	done := make(chan searchResult, 1)
	go func() {
		modules, searchErr := client.Search(context.Background(), SearchOptions{Query: "needle"})
		done <- searchResult{modules: modules, err: searchErr}
	}()

	for range maxSearchConcurrent {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("bounded description requests did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("search exceeded its description request limit")
	case <-time.After(100 * time.Millisecond):
	}

	unblock()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if len(result.modules) != candidates || maximum.Load() != maxSearchConcurrent {
			t.Fatalf("unexpected bounded search: modules=%d maximum=%d", len(result.modules), maximum.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bounded search did not complete")
	}
}

func TestClientNotFoundErrors(t *testing.T) {
	server := newDistributionServer(t, "", false)
	defer server.Close()

	client, err := NewClient(WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.Module(context.Background(), "acme/missing"); !errors.Is(err, ErrModuleNotFound) {
		t.Fatalf("expected module-not-found error, got %v", err)
	}

	if _, err := client.Version(context.Background(), "montferret/archive", "2.0.0"); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("expected version-not-found error, got %v", err)
	}
}

func TestClientCustomBasePathAndHTTPClient(t *testing.T) {
	server := newDistributionServer(t, "/registry", true)
	defer server.Close()

	transport := headerTransport{
		base:   http.DefaultTransport,
		header: "X-Barn-Test",
		value:  "configured",
	}
	client, err := NewClient(
		WithBaseURL(server.URL+"/registry"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	if err != nil {
		t.Fatal(err)
	}

	modules, err := client.Modules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 2 {
		t.Fatalf("unexpected modules: %#v", modules)
	}
}

func TestClientRejectsMalformedAndUnsupportedArtifacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		root   string
		target error
	}{
		{
			name:   "duplicate key",
			root:   `{"schemaVersion":1,"schemaVersion":1,"artifacts":{"modules":"/modules.json"}}`,
			target: ErrMalformedArtifact,
		},
		{
			name:   "trailing document",
			root:   `{"schemaVersion":1,"artifacts":{"modules":"/modules.json"}} {}`,
			target: ErrMalformedArtifact,
		},
		{
			name:   "unknown field",
			root:   `{"schemaVersion":1,"artifacts":{"modules":"/modules.json"},"extra":true}`,
			target: ErrMalformedArtifact,
		},
		{
			name:   "unsupported version",
			root:   `{"schemaVersion":2,"artifacts":{"modules":"/modules.json"}}`,
			target: ErrUnsupportedFormat,
		},
		{
			name:   "unsafe link",
			root:   `{"schemaVersion":1,"artifacts":{"modules":"https://example.org/modules.json"}}`,
			target: ErrMalformedArtifact,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(test.root))
			}))
			defer server.Close()

			client, err := NewClient(WithBaseURL(server.URL))
			if err != nil {
				t.Fatal(err)
			}

			if _, err := client.Modules(context.Background()); !errors.Is(err, test.target) {
				t.Fatalf("expected %v, got %v", test.target, err)
			}
		})
	}
}

func TestClientHTTPTransportAndCancellationErrors(t *testing.T) {
	t.Run("HTTP status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
		}))
		defer server.Close()

		client, err := NewClient(WithBaseURL(server.URL))
		if err != nil {
			t.Fatal(err)
		}

		_, err = client.Modules(context.Background())
		var httpErr *HTTPError
		if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected typed HTTP error, got %v", err)
		}
	})

	t.Run("transport", func(t *testing.T) {
		failure := errors.New("offline")
		client, err := NewClient(
			WithBaseURL("https://registry.invalid"),
			WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, failure
			})}),
		)
		if err != nil {
			t.Fatal(err)
		}

		_, err = client.Modules(context.Background())
		var transportErr *TransportError
		if !errors.As(err, &transportErr) || !errors.Is(err, failure) {
			t.Fatalf("expected wrapped transport error, got %v", err)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		client, err := NewClient(
			WithBaseURL("https://registry.invalid"),
			WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				<-request.Context().Done()

				return nil, request.Context().Err()
			})}),
		)
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = client.Modules(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	})
}

func TestClientOptionValidation(t *testing.T) {
	for _, option := range []Option{
		WithBaseURL("registry.example.org"),
		WithBaseURL("ftp://registry.example.org"),
		WithBaseURL("https://user@example.org"),
		WithHTTPClient(nil),
	} {
		if _, err := NewClient(option); err == nil {
			t.Fatal("expected invalid option to fail")
		}
	}
}

func newDistributionServer(t *testing.T, prefix string, requireHeader bool) *httptest.Server {
	t.Helper()

	routes := map[string]string{
		prefix + "/index.json": `{
  "schemaVersion": 1,
  "artifacts": {
    "modules": "./modules.json",
    "categories": "./categories.json",
    "plugins": "./plugins.json"
  }
}`,
		prefix + "/modules.json": `{
  "schemaVersion": 1,
  "modules": [
    {"id":"montferret/archive","latest":"1.0.0","href":"./records/archive.json"},
    {"id":"acme/browser","href":"./records/browser.json"}
  ]
}`,
		prefix + "/records/archive.json": `{
  "schemaVersion": 1,
  "id": "montferret/archive",
  "owner": "montferret",
  "name": "archive",
  "description": "Archive support.",
  "latest": "1.0.0",
  "versions": [
    {"version":"0.9.0","href":"./releases/0.9.0/index.json"},
    {"version":"1.0.0","href":"./releases/1.0.0/index.json"}
  ]
}`,
		prefix + "/records/browser.json": `{
  "schemaVersion": 1,
  "id": "acme/browser",
  "owner": "acme",
  "name": "browser",
  "description": "Text generation under AI::LLM with browser support.",
  "versions": [
    {"version":"1.0.0-rc.1","href":"./releases/1.0.0-rc.1/index.json"}
  ]
}`,
		prefix + "/records/releases/1.0.0/index.json": fmt.Sprintf(`{
  "schemaVersion": 1,
  "id": "montferret/archive",
  "version": "1.0.0",
  "namespace": "ARCHIVE",
  "ferret": ">=2.0.0 <3.0.0",
  "source": {
    "repository": "https://example.com/archive.git",
    "path": "modules/archive",
    "commit": %q
  },
  "content": {"documentation":"./docs.md"}
}`, strings.Repeat("a", 40)),
		prefix + "/categories.json": `{
  "schemaVersion": 1,
  "categories": [
    {"id":"files","name":"Files","count":2,"href":"./category-files.json"},
    {"id":"ai","name":"AI","count":0,"href":"./category-ai.json"}
  ]
}`,
		prefix + "/category-files.json": `{
  "schemaVersion": 1,
  "category": {"id":"files","name":"Files"},
  "modules": [
    {"id":"montferret/archive","latest":"1.0.0","href":"./records/archive.json"},
    {"id":"acme/browser","href":"./records/browser.json"}
  ]
}`,
	}

	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if requireHeader && request.Header.Get("X-Barn-Test") != "configured" {
			http.Error(response, "missing configured client header", http.StatusBadRequest)
			return
		}

		document, exists := routes[request.URL.Path]
		if !exists {
			http.NotFound(response, request)
			return
		}

		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(document))
	}))
}

func newSingleModuleSearchServer(t *testing.T, detail http.HandlerFunc) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/index.json":
			_, _ = response.Write([]byte(`{"schemaVersion":1,"artifacts":{"modules":"/modules.json"}}`))
		case "/modules.json":
			_, _ = response.Write([]byte(`{"schemaVersion":1,"modules":[{"id":"acme/browser","href":"/browser.json"}]}`))
		case "/browser.json":
			if detail != nil {
				detail(response, request)

				return
			}

			_, _ = response.Write([]byte(`{
  "schemaVersion": 1,
  "id": "acme/browser",
  "owner": "acme",
  "name": "browser",
  "description": "Needle support.",
  "versions": [{"version":"1.0.0","href":"/browser/1.0.0.json"}]
}`))
		default:
			http.NotFound(response, request)
		}
	}))
}

type headerTransport struct {
	base   http.RoundTripper
	header string
	value  string
}

func (transport headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set(transport.header, transport.value)

	return transport.base.RoundTrip(request)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
