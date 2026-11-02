# DOCUMENT::PDF Module

`github.com/MontFerret/contrib/modules/document/pdf` registers read-only PDF helpers under the `DOCUMENT::PDF` namespace.

The module opens PDFs through Ferret's filesystem abstraction, exposes lazy document and page handles, extracts best-effort text, reports basic page dimensions, and returns low-level positioned text fragments. `DOCUMENT::PDF::OPEN` is the only namespace function; document and page data are read through host-value properties.

The implementation uses `github.com/ledongthuc/pdf` internally, but that dependency is hidden behind module-owned core types so the public FQL API can remain stable if the parser is replaced later.

Out of scope for this version: PDF creation or modification, page insertion or deletion, form filling, annotation editing, digital signatures, password management, OCR, table recognition, paragraph or heading detection, metadata extraction, link extraction, image extraction, rendering, PDF-to-HTML conversion, remote HTTP URLs, and aliases under `PDF::`.

## Install

```sh
go get github.com/MontFerret/contrib/modules/document/pdf
```

## Register The Module

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	pdfmodule "github.com/MontFerret/contrib/modules/document/pdf"
)

func main() {
	engine, err := ferret.New(
		ferret.WithFSRoot("./documents"),
		ferret.WithModules(pdfmodule.New()),
	)
	if err != nil {
		panic(err)
	}

	_ = engine
}
```

`DOCUMENT::PDF::OPEN` uses Ferret's filesystem from the execution context. Configure `ferret.WithFSRoot` in the host application and use paths relative to that root. The module does not open unrestricted host paths directly.

When the filesystem source cannot provide random access, the module buffers the PDF in memory and passes a `bytes.Reader` to the parser. The default buffer limit is 64 MiB and can be configured at registration time:

```go
ferret.WithModules(pdfmodule.New(pdfmodule.WithMaxBufferSize(128 * 1024 * 1024)))
```

## API Reference

| Function | Signature | Returns | Notes |
| --- | --- | --- | --- |
| `DOCUMENT::PDF::OPEN` | `OPEN(path)` | Document handle | Opens an existing PDF through Ferret's filesystem. |

| Document property | Returns | Notes |
| --- | --- | --- |
| `document.pageCount` | `Int` | Returns the document page count. |
| `document.pages` | Page collection | Lazy iterable and zero-indexed page collection. |

| Page collection behavior | Returns | Notes |
| --- | --- | --- |
| `document.pages[0]` | Page handle or `NONE` | Uses normal zero-based FQL indexing. |
| `LENGTH(document.pages)` | `Int` | Returns the number of pages. |
| `FOR page IN document.pages` | Page handles | Creates page values lazily while iterating. |

| Page property | Returns | Notes |
| --- | --- | --- |
| `page.number` | `Int` | One-based page number. |
| `page.width` | `Float` | Page width in PDF points. |
| `page.height` | `Float` | Page height in PDF points. |
| `page.rotation` | `Int` | Normalized page rotation. |
| `page.text` | `String` | Extracts best-effort text when accessed. |
| `page.blocks` | `Array<Object>` | Extracts low-level positioned text fragments when accessed. |

## Examples

```fql
LET document = DOCUMENT::PDF::OPEN("./report.pdf")
RETURN {
  pageCount: document.pageCount,
  firstText: document.pages[0].text
}
```

```fql
LET document = DOCUMENT::PDF::OPEN("./report.pdf")
RETURN (
  FOR page IN document.pages
    RETURN {
      number: page.number,
      width: page.width,
      height: page.height,
      rotation: page.rotation,
      text: page.text,
      blocks: page.blocks
    }
)
```

```fql
LET document = DOCUMENT::PDF::OPEN("./report.pdf")
RETURN {
  count: LENGTH(document.pages),
  firstBlocks: document.pages[0].blocks,
  outOfRange: document.pages[999]
}
```

## Page And Coordinate Conventions

`document.pages` is lazy. Reading the collection or iterating page values does not extract text or positioned blocks. `page.text` and `page.blocks` perform extraction when those properties are accessed.

Collection indexes are zero-based, so `document.pages[0]` returns the first page. `page.number` remains one-based for PDF-domain display and consistency.

Page dimensions and text bounds are in PDF points. For page dimensions, the module prefers a valid crop box and falls back to the media box. Positioned text coordinates use the coordinate system returned by `github.com/ledongthuc/pdf`: X increases left to right, Y increases bottom to top, and the origin is bottom-left. The module does not convert coordinates.

`page.blocks` returns positioned text entries from the parser, not semantic paragraphs, headings, table cells, or layout regions. Empty and whitespace-only entries are omitted.

## Text Extraction Limitations

Text extraction is best-effort. Reading order can be wrong for multi-column or heavily positioned documents, custom font encodings can produce missing or incorrect characters, and malformed or uncommon PDFs can fail to parse. Scanned pages without embedded text usually return an empty string. OCR is not performed.

Password-protected and encrypted PDFs may not be supported reliably by the underlying parser. Metadata and link APIs are planned follow-up functionality and are not registered as stubs in this version.

## Lifecycle And Concurrency

Document handles retain the source reader or backing buffer while open. Page handles retain a reference to their parent document and become invalid after the document is closed. Document cleanup is safe and idempotent.

Document, page collection, and page operations guard shared open/closed state and are safe for ordinary concurrent reads. Cancellation is cooperative between major operations, including open, page extraction, and positioned-text iteration.
