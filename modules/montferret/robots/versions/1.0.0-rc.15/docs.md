# WEB::ROBOTS Module

`github.com/MontFerret/contrib/modules/web/robots` registers robots.txt parsing and policy helpers under the `WEB::ROBOTS` namespace for Ferret hosts.

The module exposes these functions:

- `WEB::ROBOTS::PARSE`
- `WEB::ROBOTS::ALLOWS`
- `WEB::ROBOTS::MATCH`
- `WEB::ROBOTS::SITEMAPS`

## Install

```sh
go get github.com/MontFerret/contrib/modules/web/robots
```

## Register The Module

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	robotsmodule "github.com/MontFerret/contrib/modules/web/robots"
)

func main() {
	robotsMod, err := robotsmodule.New()
	if err != nil {
		panic(err)
	}

	engine, err := ferret.New(
		ferret.WithModules(robotsMod),
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
| `WEB::ROBOTS::PARSE` | `WEB::ROBOTS::PARSE(text)` | `Object` | Parses raw robots.txt text into a plain Ferret object. |
| `WEB::ROBOTS::ALLOWS` | `WEB::ROBOTS::ALLOWS(robots, path, userAgent?)` | `Boolean` | Returns whether the path is allowed for the effective user-agent group. |
| `WEB::ROBOTS::MATCH` | `WEB::ROBOTS::MATCH(robots, path, userAgent?)` | `Object` | Returns rule-match details for debugging and inspection. |
| `WEB::ROBOTS::SITEMAPS` | `WEB::ROBOTS::SITEMAPS(robots)` | `String[]` | Returns top-level sitemap declarations from the robots document. |

## Return Shapes

`WEB::ROBOTS::PARSE` returns an object in this shape:

```json
{
  "groups": [
    {
      "userAgents": ["*"],
      "allow": ["/public"],
      "disallow": ["/admin"],
      "crawlDelay": 5
    }
  ],
  "sitemaps": [
    "https://example.com/sitemap.xml"
  ],
  "host": null
}
```

`WEB::ROBOTS::MATCH` returns an object in this shape:

```json
{
  "allowed": true,
  "directive": "allow",
  "pattern": "/products/",
  "userAgent": "FerretBot"
}
```

`userAgent` reports the normalized debug token used for evaluation. Exact matches, no-match default allows, and implicit `/robots.txt` allows return the normalized requested token. When evaluation falls back to wildcard groups, it is returned as `"*"`. The field does not preserve the original `User-agent:` casing from the robots file. When access is allowed by default with no matching rule, `directive` and `pattern` are returned as `null`.

## Examples

### Parse A robots.txt Document

```fql
LET robots = WEB::ROBOTS::PARSE($text)
RETURN robots.sitemaps
```

### Check Path Access

```fql
LET robots = WEB::ROBOTS::PARSE($text)
RETURN WEB::ROBOTS::ALLOWS(robots, "/admin/users", "FerretBot")
```

### Inspect The Matching Rule

```fql
LET robots = WEB::ROBOTS::PARSE($text)
RETURN WEB::ROBOTS::MATCH(robots, "/catalog/item/1", "FerretBot")
```

### Return Declared Sitemap URLs

```fql
LET robots = WEB::ROBOTS::PARSE($text)
FOR sitemap IN WEB::ROBOTS::SITEMAPS(robots)
  RETURN sitemap
```

## Behavior Notes

- User-agent matching is case-insensitive and exact against the supplied crawler product token.
- If no exact user-agent group matches, `*` groups are used when present.
- Matching supports `*` wildcards and trailing `$` end anchors.
- The most specific rule wins; when equal `Allow` and `Disallow` rules both match, `Allow` wins.
- If no rule matches, the path is allowed.
- `/robots.txt` is always allowed.
- `PARSE` preserves group order and per-directive rule order.
- `host` is exposed for transparency only and does not affect matching.
