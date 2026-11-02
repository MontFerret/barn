# TOML Module

`github.com/MontFerret/contrib/modules/toml` registers TOML helpers under the canonical `TOML` namespace for Ferret hosts.

The module exposes these canonical functions:

- `TOML::DECODE`
- `TOML::ENCODE`

## Install

```sh
go get github.com/MontFerret/contrib/modules/toml
```

## Register The Module

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	tomlmodule "github.com/MontFerret/contrib/modules/toml"
)

func main() {
	tomlMod, err := tomlmodule.New()
	if err != nil {
		panic(err)
	}

	engine, err := ferret.New(
		ferret.WithModules(tomlMod),
	)
	if err != nil {
		panic(err)
	}

	_ = engine
}
```

## Function Reference

| Function | Signature                        | Returns | Notes |
| --- |----------------------------------| --- | --- |
| `TOML::DECODE` | `TOML::DECODE(input[, options])` | `Object` | Decodes a whole TOML document from string or binary input. |
| `TOML::ENCODE` | `TOML::ENCODE(value[, options])` | `String` | Encodes a Ferret object into TOML text. |

## Decode Options

```fql
{
  datetime: "string" | "native",
  strict: true | false
}
```

Default:

```fql
{
  datetime: "string",
  strict: true
}
```

## Encode Options

```fql
{
  sortKeys: true | false,
  datetime: "rfc3339" | "preserve"
}
```

Default:

```fql
{
  sortKeys: false,
  datetime: "rfc3339"
}
```

## Examples

### Decode

```fql
RETURN TOML::DECODE("
title = \"Ferret\"

[server]
host = \"localhost\"
port = 8080
")
```

### Encode

```fql
RETURN TOML::ENCODE({
  title: "Ferret",
  server: {
    host: "localhost",
    port: 8080
  }
})
```

## Behavior Notes

- `TOML::DECODE` accepts both string and binary TOML input.
- The top-level TOML document always decodes to an object.
- `datetime: "string"` returns TOML timestamps as canonical strings.
- `datetime: "native"` returns Ferret `DateTime` values and preserves TOML local datetime/date/time flavor using the underlying location name.
- `strict: false` is reserved for a future relaxed mode and currently returns an explicit error.
- `TOML::ENCODE` requires the top-level value to be an object or map.
- Arrays of objects encode as arrays of tables when they appear as direct object fields.
- Values without a TOML representation, such as `None`, binary data, iterators, and host-only values, return an explicit error.
- Output formatting is implementation-defined apart from valid TOML syntax and the optional `sortKeys` behavior.
