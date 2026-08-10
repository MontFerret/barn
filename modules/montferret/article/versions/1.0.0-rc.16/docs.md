# WEB::ARTICLE Module

`github.com/MontFerret/contrib/modules/web/article` registers article extraction helpers under the `WEB::ARTICLE` namespace for Ferret hosts.

The module exposes these functions:

- `WEB::ARTICLE::EXTRACT`
- `WEB::ARTICLE::TEXT`
- `WEB::ARTICLE::MARKDOWN`

## Install

```sh
go get github.com/MontFerret/contrib/modules/web/article
```

## Register The Module

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	articlemodule "github.com/MontFerret/contrib/modules/web/article"
)

func main() {
	articleMod, err := articlemodule.New()
	if err != nil {
		panic(err)
	}

	engine, err := ferret.New(
		ferret.WithModules(articleMod),
	)
	if err != nil {
		panic(err)
	}

	_ = engine
}
```

## Function Reference

| Function | Signature | Returns | Notes |
| --- | --- | --- | --- |
| `WEB::ARTICLE::EXTRACT` | `WEB::ARTICLE::EXTRACT(input)` | `Object` | Extracts a normalized article object from raw HTML, `HTMLPage`, `HTMLDocument`, or `HTMLElement`. |
| `WEB::ARTICLE::TEXT` | `WEB::ARTICLE::TEXT(input)` | `String \| None` | Returns the cleaned main article text when meaningful content is found. |
| `WEB::ARTICLE::MARKDOWN` | `WEB::ARTICLE::MARKDOWN(input)` | `String \| None` | Returns the cleaned article body rendered as Markdown when available. |

## Return Shape

`WEB::ARTICLE::EXTRACT` always returns an object with these fields:

```json
{
  "title": "Example title",
  "byline": "Jane Doe",
  "excerpt": "Short description or summary",
  "siteName": "Example Site",
  "publishedAt": "2026-03-30T10:00:00Z",
  "updatedAt": "2026-03-30T12:00:00Z",
  "lang": "en",
  "dir": "ltr",
  "canonicalUrl": "https://example.com/post",
  "leadImage": "https://example.com/image.jpg",
  "text": "Clean main article text",
  "html": "<p>Sanitized article body</p>",
  "markdown": "Body rendered as Markdown",
  "wordCount": 1234,
  "readingTimeMinutes": 7,
  "tags": ["ai", "news"],
  "categories": ["Technology"]
}
```

Missing values are returned as `null`.

## Examples

### Extract A Normalized Article

```fql
LET response = HTTP::GET($url)
RETURN WEB::ARTICLE::EXTRACT(response.body)
```

### Extract From A JS-Rendered Page

```fql
LET page = HTML::DOCUMENT($url, true)
RETURN WEB::ARTICLE::EXTRACT(page)
```

### Get Clean Article Text

```fql
RETURN WEB::ARTICLE::TEXT($html)
```

### Render Markdown For Indexing Or Export

```fql
RETURN {
  url: $url,
  markdown: WEB::ARTICLE::MARKDOWN(HTTP::GET($url).body)
}
```

## Behavior Notes

- Extraction is heuristic and best-effort; malformed HTML is parsed when practical.
- `input` may be raw HTML, `HTMLPage`, `HTMLDocument`, or `HTMLElement`.
- `EXTRACT` may still return metadata when no meaningful article body is found.
- `TEXT` and `MARKDOWN` return `null` when the page is parseable but not article-like enough.
- For `HTMLPage` and `HTMLDocument` inputs, the page URL is used as the fallback base URL when the DOM does not contain `<base href>`.
- URL metadata is resolved to absolute URLs whenever a base URL is available (from `<base href>` or the page URL for `HTMLPage`/`HTMLDocument`); for raw HTML or `HTMLElement` inputs without a base URL, relative URL values are preserved.
- Timestamps are normalized to RFC3339 UTC when parseable; otherwise the original trimmed value is preserved.
- `html` is sanitized with an allowlist policy before it is returned, so dangerous attributes and URL schemes are stripped from the article body fragment.
- `markdown` is rendered from that sanitized body HTML.
- `text`, `html`, and `markdown` contain the cleaned body only and do not prepend the title.
