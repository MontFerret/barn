# WEB::SITEMAP Module

`github.com/MontFerret/contrib/modules/web/sitemap` registers sitemap discovery helpers under the `WEB::SITEMAP` namespace for Ferret hosts.

The module exposes these functions:

- `WEB::SITEMAP::FETCH`
- `WEB::SITEMAP::URLS`
- `WEB::SITEMAP::STREAM`

## Install

```sh
go get github.com/MontFerret/contrib/modules/web/sitemap
```

## Register The Module

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	sitemapmodule "github.com/MontFerret/contrib/modules/web/sitemap"
)

func main() {
	sitemapMod, err := sitemapmodule.New()
	if err != nil {
		panic(err)
	}

	engine, err := ferret.New(
		ferret.WithModules(sitemapMod),
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
| `WEB::SITEMAP::FETCH` | `WEB::SITEMAP::FETCH(url, opts?)` | `Object` | Fetches and parses a single sitemap document. Returns either a `urlset` or `sitemapindex` object. |
| `WEB::SITEMAP::URLS` | `WEB::SITEMAP::URLS(url, opts?)` | `Object[]` | Fetches a sitemap tree and returns flattened URL entries. |
| `WEB::SITEMAP::STREAM` | `WEB::SITEMAP::STREAM(url, opts?)` | `Iterator<Object>` | Lazily fetches and expands sitemap trees. |

## Return Shapes

`WEB::SITEMAP::FETCH` returns one of these document shapes:

```json
{
  "type": "urlset",
  "urls": [
    {
      "loc": "https://example.com/page",
      "lastmod": "2026-03-01T12:00:00Z",
      "changefreq": "weekly",
      "priority": 0.8,
      "source": "https://example.com/sitemap.xml"
    }
  ]
}
```

```json
{
  "type": "sitemapindex",
  "sitemaps": [
    {
      "loc": "https://example.com/posts.xml",
      "lastmod": "2026-03-01T12:00:00Z"
    }
  ]
}
```

`WEB::SITEMAP::URLS` and `WEB::SITEMAP::STREAM` yield URL entry objects in this shape:

```json
{
  "loc": "https://example.com/page",
  "lastmod": "2026-03-01T12:00:00Z",
  "changefreq": "weekly",
  "priority": 0.8,
  "source": "https://example.com/sitemap.xml"
}
```

`lastmod` is returned as `String | None` in v1.

## Options

All sitemap functions accept the same camelCase options object.

| Option | Default | Applies To | Notes |
| --- | --- | --- | --- |
| `recursive` | `true` | urls, stream | Expands nested sitemap indexes. `FETCH` accepts the option for API consistency but does not recurse. |
| `dedupe` | `true` | urls, stream | Deduplicates yielded page URLs by `loc` and avoids refetching the same sitemap document. |
| `maxDepth` | `8` | urls, stream | Maximum nested sitemap-index depth. |
| `timeout` | `30000` | fetch, urls, stream | Per-request timeout in milliseconds. |
| `headers` | `{}` | fetch, urls, stream | Optional HTTP headers such as `User-Agent`. |

When `recursive` is `false`, `URLS` and `STREAM` return no URL entries for sitemap-index documents.

## Examples

### Inspect A Sitemap Document

```fql
RETURN WEB::SITEMAP::FETCH("https://example.com/sitemap.xml")
```

### Return All Discovered URLs

```fql
FOR page IN WEB::SITEMAP::STREAM("https://example.com/sitemap.xml")
  RETURN page.loc
```

### Filter URLs From A Sitemap Tree

```fql
FOR page IN WEB::SITEMAP::URLS("https://example.com/sitemap.xml")
  FILTER CONTAINS(page.loc, "/blog/")
  RETURN page
```

### Limit Large Sitemap Traversals

```fql
FOR page IN WEB::SITEMAP::STREAM(
  "https://example.com/sitemap.xml",
  {
    headers: { "User-Agent": "Ferret/2" },
    maxDepth: 4
  }
)
  LIMIT 100
  RETURN page.loc
```

## Behavior Notes

- `FETCH` validates sitemap URLs and supports only `http` and `https`.
- `URLS` and `STREAM` traverse nested sitemap indexes depth-first.
- When dedupe is enabled, duplicate page URLs are removed by `loc` and the first-seen entry is preserved.
- Fetch, parse, and expansion failures include sitemap URL and stage context in the error.
- `priority` is parsed as a float when present; malformed values return an error.
- XML parsing is implemented on top of the existing contrib XML internals.
