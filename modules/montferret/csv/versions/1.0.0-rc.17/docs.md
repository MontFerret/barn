# CSV Module

`github.com/MontFerret/contrib/modules/csv` registers CSV helpers under the `CSV` namespace for Ferret hosts.

The module exposes these functions:

- `CSV::DECODE`
- `CSV::DECODE_ROWS`
- `CSV::DECODE_STREAM`
- `CSV::DECODE_ROWS_STREAM`
- `CSV::ENCODE`

## Install

```sh
go get github.com/MontFerret/contrib/modules/csv
```

## Register The Module

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	csvmodule "github.com/MontFerret/contrib/modules/csv"
)

func main() {
	csvMod, err := csvmodule.New()
	if err != nil {
		panic(err)
	}

	engine, err := ferret.New(
		ferret.WithModules(csvMod),
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
| `CSV::DECODE` | `CSV::DECODE(data, opts?)` | `Object[]` | Decodes CSV text into objects. By default, the first row is treated as a header row. |
| `CSV::DECODE_ROWS` | `CSV::DECODE_ROWS(data, opts?)` | `Any[][]` | Decodes CSV text into raw row arrays. Header rows stay in the output as data. |
| `CSV::DECODE_STREAM` | `CSV::DECODE_STREAM(data, opts?)` | `Iterator<Object>` | Streaming object decoder. Accepts string or binary CSV input. |
| `CSV::DECODE_ROWS_STREAM` | `CSV::DECODE_ROWS_STREAM(data, opts?)` | `Iterator<Any[]>` | Streaming row decoder. Accepts string or binary CSV input. |
| `CSV::ENCODE` | `CSV::ENCODE(data, opts?)` | `String` | Encodes an array of objects or row arrays into CSV text. |

## Options

All options use camelCase keys when passed from Ferret queries.

| Option | Default | Applies To | Notes |
| --- | --- | --- | --- |
| `delimiter` | `","` | decode, encode | Field delimiter. Must be exactly one valid character. |
| `comment` | unset | decode | Skips comment-prefixed records during decode. Must differ from `delimiter`. |
| `columns` | unset | decode, encode | Decode: explicit object field names when `header` is `false`. Encode: explicit object column order and selection. |
| `nullValues` | unset | decode | Decodes matching field values as `null`/`None`. |
| `header` | `true` | decode, encode | Decode: uses the first record as object keys. Encode: writes a header row when encoding objects. |
| `trim` | `false` | decode | Trims leading and trailing whitespace before value conversion. |
| `skipEmpty` | `true` | decode | Skips completely empty records. |
| `strict` | `true` | decode | Enforces consistent field counts and strict header validation. |
| `inferTypes` | `false` | decode | Converts values to numbers and booleans when possible; otherwise keeps strings. |

## Examples

### Decode Objects From A Headered CSV

```fql
RETURN CSV::DECODE("name,age\nAlice,30\nBob,25")
```

This returns objects keyed by the header row:

```json
[
  { "name": "Alice", "age": "30" },
  { "name": "Bob", "age": "25" }
]
```

### Decode A Headerless CSV With Explicit Columns

```fql
RETURN CSV::DECODE(
  "Alice,Smith\nBob,Jones",
  {
    header: false,
    columns: ["first", "last"]
  }
)
```

### Decode Raw Rows

```fql
RETURN CSV::DECODE_ROWS("name,age\nAlice,30")
```

This keeps the header row in the output:

```json
[
  ["name", "age"],
  ["Alice", "30"]
]
```

### Encode Objects With Explicit Column Order

```fql
RETURN CSV::ENCODE(
  [
    { name: "Alice", age: 30 },
    { name: "Bob", age: 25 }
  ],
  {
    columns: ["age", "name"]
  }
)
```

This writes the header and data in the requested order:

```text
age,name
30,Alice
25,Bob
```

### Stream Decoded Objects

```fql
FOR row IN CSV::DECODE_STREAM(
  "name,age\nAlice,30\nBob,25",
  { inferTypes: true }
)
RETURN row
```

Streaming decode functions accept either string or binary CSV input.

## Behavior Notes

- `header: true` cannot be combined with `columns`.
- When decoding objects with `header: false` and no `columns`, the module auto-generates `col1`, `col2`, and so on.
- `strict: false` relaxes field-count validation during decode.
- In relaxed header mode, empty header names are synthesized and duplicate header names are renamed to unique suffixed variants such as `name_2`.
- Stream iterator keys preserve the original 1-based CSV record numbers after parsing.
- `comment` must differ from `delimiter`, and both options must be exactly one valid character.
