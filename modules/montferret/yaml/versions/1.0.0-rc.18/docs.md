# YAML Module

`github.com/MontFerret/contrib/modules/yaml` registers YAML helpers under the `YAML` namespace for Ferret hosts.

The module exposes these functions:

- `YAML::DECODE`
- `YAML::DECODE_ALL`
- `YAML::ENCODE`

## Install

```sh
go get github.com/MontFerret/contrib/modules/yaml
```

## Register The Module

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	yamlmodule "github.com/MontFerret/contrib/modules/yaml"
)

func main() {
	yamlMod, err := yamlmodule.New()
	if err != nil {
		panic(err)
	}

	engine, err := ferret.New(
		ferret.WithModules(yamlMod),
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
| `YAML::DECODE` | `YAML::DECODE(data)` | `Any` | Decodes exactly one YAML document. Rejects streams with multiple documents. |
| `YAML::DECODE_ALL` | `YAML::DECODE_ALL(data)` | `Any[]` | Decodes all YAML documents from a YAML stream in order. |
| `YAML::ENCODE` | `YAML::ENCODE(value)` | `String` | Encodes a supported Ferret runtime value into YAML text. |

## Type Mapping

Decoded YAML values map to Ferret runtime values as follows:

| YAML | Ferret |
| --- | --- |
| mapping | `Object` |
| sequence | `Array` |
| string | `String` |
| integer | `Int` |
| float | `Float` |
| boolean | `Boolean` |
| null | `None` |

## Examples

### Decode A Single Document

```fql
RETURN YAML::DECODE("
name: Alice
age: 30
active: true
")
```

### Decode A Multi-Document Stream

```fql
RETURN YAML::DECODE_ALL("
---
name: Alice
---
- 1
- 2
")
```

### Encode A Value

```fql
RETURN YAML::ENCODE({
  name: "Alice",
  age: 30,
  tags: ["yaml", "ferret"]
})
```

## Behavior Notes

- `YAML::DECODE` and `YAML::DECODE_ALL` accept both string and binary YAML input.
- Empty input is rejected by both decode functions.
- `YAML::DECODE` rejects YAML streams that contain more than one document.
- Anchors, aliases, and merge keys are accepted only when they resolve to ordinary data values.
- `YAML::ENCODE` supports only plain data values; unsupported runtime types such as binary, datetime, and iterator-like values return an error.
- Output formatting is implementation-defined in v1.
