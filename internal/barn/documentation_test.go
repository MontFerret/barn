package barn

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
)

func TestRenderDocumentationProducesDeterministicSafeHTML(t *testing.T) {
	source := []byte(`# Archive

## Install

## Install

[Guide](guides/start.md)
[Local](#install)
![Diagram](images/diagram.png)

| Name | Value |
| --- | --- |
| one | two |

- [x] Published

` + "```go\nfunc main() {}\n```" + `

<script>alert("unsafe")</script>
<a href="javascript:alert(1)" onclick="alert(1)" style="color:red">Unsafe</a>
<img src="javascript:alert(1)" onerror="alert(1)">
`)
	baseURL := "https://docs.example.org/modules/archive/"

	first, err := renderDocumentation(source, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderDocumentation(source, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("rendered documentation is not deterministic")
	}

	html := string(first)
	for _, expected := range []string{
		`<h1 id="archive">Archive</h1>`,
		`<h2 id="install">Install</h2>`,
		`<h2 id="install-1">Install</h2>`,
		`href="https://docs.example.org/modules/archive/guides/start.md"`,
		`href="#install"`,
		`src="https://docs.example.org/modules/archive/images/diagram.png"`,
		`<table>`,
		`class="language-go"`,
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("rendered documentation does not contain %q:\n%s", expected, html)
		}
	}

	for _, unsafe := range []string{"<script", "javascript:", "onclick", "onerror", "style="} {
		if strings.Contains(strings.ToLower(html), unsafe) {
			t.Errorf("rendered documentation contains unsafe value %q:\n%s", unsafe, html)
		}
	}
}

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}

	return parsed
}

func TestResolveDocumentationLinkPreservesAbsoluteAndFragmentLinks(t *testing.T) {
	base := mustURL(t, "https://docs.example.org/modules/archive/")
	for input, want := range map[string]string{
		"#install":                    "#install",
		"https://example.org/guide":   "https://example.org/guide",
		"mailto:team@example.org":     "mailto:team@example.org",
		"../shared/compatibility.md":  "https://docs.example.org/modules/shared/compatibility.md",
		"/reference/module-contracts": "https://docs.example.org/reference/module-contracts",
	} {
		if got := string(resolveDocumentationLink([]byte(input), base)); got != want {
			t.Errorf("resolve %q = %q, want %q", input, got, want)
		}
	}
}
