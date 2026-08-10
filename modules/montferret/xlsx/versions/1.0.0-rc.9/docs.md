# DOCUMENT::XLSX Module

`github.com/MontFerret/contrib/modules/document/xlsx` registers Microsoft Excel-compatible `.xlsx` workbook helpers under the `DOCUMENT::XLSX` namespace.

The module focuses on workbook lifecycle, worksheets, scalar cell values, rectangular ranges, row appends, persistence, and worksheet querying. It does not expose the underlying Go Excel library through Ferret.

Out of scope for the initial version: `.xls`, macros or macro execution, formula evaluation, charts, pivot tables, images, conditional formatting, comprehensive styling, password-protected workbooks, CSV, and a generic spreadsheet abstraction.

## Install

```sh
go get github.com/MontFerret/contrib/modules/document/xlsx
```

## Register The Module

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	xlsxmodule "github.com/MontFerret/contrib/modules/document/xlsx"
)

func main() {
	engine, err := ferret.New(
		ferret.WithFSRoot("./workbooks"),
		ferret.WithModules(xlsxmodule.New()),
	)
	if err != nil {
		panic(err)
	}

	_ = engine
}
```

The module is importable by embedded applications and by external Ferret plugin packaging. This repository does not add it to a default CLI binary or define a plugin manifest.

`OPEN`, `SAVE`, and `SAVE_AS` use Ferret's filesystem from the execution context. Configure `ferret.WithFSRoot` in the host application to allow disk access, and use workbook paths relative to that root. `ferret.WithFSReadOnly` blocks writes through this module.

## Function Reference

| Function | Signature | Returns | Notes |
| --- | --- | --- | --- |
| `DOCUMENT::XLSX::CREATE` | `CREATE()` | Workbook handle | Creates an in-memory workbook. |
| `DOCUMENT::XLSX::OPEN` | `OPEN(path)` | Workbook handle | Opens an existing `.xlsx` through Ferret's filesystem; never creates on failure. |
| `DOCUMENT::XLSX::SHEETS` | `SHEETS(workbook)` | `Array<String>` | Worksheet names in workbook order. |
| `DOCUMENT::XLSX::SHEET` | `SHEET(workbook, name)` | Worksheet handle | Fails when the sheet does not exist. |
| `DOCUMENT::XLSX::ADD_SHEET` | `ADD_SHEET(workbook, name)` | Worksheet handle | Creates a worksheet. |
| `DOCUMENT::XLSX::DELETE_SHEET` | `DELETE_SHEET(workbook, name)` | `Boolean` | Deletes a worksheet and invalidates existing handles to it. |
| `DOCUMENT::XLSX::GET` | `GET(sheet, cell)` | Scalar or `NONE` | Reads one A1 cell. |
| `DOCUMENT::XLSX::SET` | `SET(sheet, cell, value)` | `Boolean` | Writes one scalar value. |
| `DOCUMENT::XLSX::RANGE` | `RANGE(sheet, range)` | Row arrays | Reads a rectangular A1 range. |
| `DOCUMENT::XLSX::WRITE_RANGE` | `WRITE_RANGE(sheet, rangeOrCell, rows)` | `Boolean` | Writes a rectangular array of row arrays. |
| `DOCUMENT::XLSX::APPEND` | `APPEND(sheet, rows)` | `Boolean` | Appends after the last populated row. |
| `DOCUMENT::XLSX::SAVE` | `SAVE(workbook)` | `Boolean` | Saves to the current path. Created workbooks need `SAVE_AS` first. |
| `DOCUMENT::XLSX::SAVE_AS` | `SAVE_AS(workbook, path)` | `Boolean` | Saves through Ferret's filesystem and makes `path` the current path. Parent directories must already exist. |
| `DOCUMENT::XLSX::CLOSE` | `CLOSE(workbook)` | `Boolean` | Releases resources. Repeated close is idempotent. |

## Reading And Writing

```fql
LET workbook = DOCUMENT::XLSX::CREATE()
LET sheet = DOCUMENT::XLSX::SHEET(workbook, "Sheet1")

DOCUMENT::XLSX::SET(sheet, "A1", "Name")
DOCUMENT::XLSX::SET(sheet, "B1", "Score")
DOCUMENT::XLSX::WRITE_RANGE(sheet, "A2", [
  ["Alice", 92],
  ["Bob", 81]
])

RETURN DOCUMENT::XLSX::RANGE(sheet, "A1:B3")
```

`WRITE_RANGE` accepts either a start cell (`"A1"`) or a full range (`"A1:B3"`). A full range must match the supplied matrix dimensions. Rows must be rectangular; errors include the invalid one-based row index.

`APPEND` finds the last row with at least one non-empty value according to the workbook's current worksheet values. It ignores intentionally empty trailing rows and style-only rows.

## Querying Worksheets

Worksheet handles are queryable. The query payload is a string A1 range:

```fql
LET rows = QUERY "A1:D20" IN sheet
LET sameRows = sheet[~ "A1:D20"]
```

Without `WITH.headers`, queries return row arrays. With `headers: true`, the first row becomes object keys and is not returned as data:

```fql
RETURN QUERY "A1:D100" IN sheet WITH {
  headers: true,
  trimEmptyRows: true
}
```

Header conversion is deterministic:

- header values are converted with their Ferret string representation;
- empty header cells become `column_1`, `column_2`, and so on by position;
- duplicate names are disambiguated as `name_2`, `name_3`, and so on.

`trimEmptyRows: true` removes only fully empty rows at the end of the selected range. It does not remove empty rows in the middle.

## Complete Example

```fql
LET workbook = DOCUMENT::XLSX::OPEN("./sales.xlsx")
LET source = DOCUMENT::XLSX::SHEET(workbook, "Sales")
LET output = DOCUMENT::XLSX::ADD_SHEET(workbook, "Active Sales")

LET activeRows = (
  FOR row IN (
    QUERY "A1:D100" IN source WITH {
      headers: true,
      trimEmptyRows: true
    }
  )
    FILTER row.Active == true
    RETURN [
      row.Name,
      row.Department,
      row.Score
    ]
)

DOCUMENT::XLSX::WRITE_RANGE(output, "A1", UNION(
  [["Name", "Department", "Score"]],
  activeRows
))
DOCUMENT::XLSX::SAVE_AS(workbook, "./output/active-sales.xlsx")
DOCUMENT::XLSX::CLOSE(workbook)
```

## Type Conversion

Reads return deterministic scalar Ferret values:

- empty cell -> `NONE`;
- string -> `String`;
- boolean -> `Boolean`;
- integer-like number within range -> `Int`;
- other number -> `Float`;
- explicit Excel date cells or cells with built-in date/time styles -> `DateTime` when the serial value can be decoded.

Writes accept `NONE`, strings, booleans, integers, floats, and `DateTime`. Arrays, objects, binary values, functions, and host values are rejected rather than serialized implicitly.

Formula cells return the cached/displayed value available in the file. The module does not implement an Excel calculation engine, so formulas created or changed by other tools may need to be recalculated by Excel-compatible software before cached values are current.

## Lifecycle And Concurrency

Workbooks own their worksheet values. Closing a workbook invalidates all worksheets derived from it. Deleting a worksheet invalidates existing handles for that worksheet, and re-creating a worksheet with the same name does not revive old handles.

Workbook operations are guarded by an internal mutex. Calls through a single workbook handle are serialized because the underlying workbook object is mutable.
