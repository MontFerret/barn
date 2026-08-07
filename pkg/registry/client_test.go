package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
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

	results, err = client.Search(context.Background(), SearchOptions{Category: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if results == nil || len(results) != 0 {
		t.Fatalf("unknown category should return an empty non-nil result: %#v", results)
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
