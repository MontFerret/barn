# XML Module

`github.com/MontFerret/contrib/modules/xml` registers XML helpers under the `XML` namespace for Ferret hosts.

The module exposes these functions:

- `XML::ROOT`
- `XML::TEXT`
- `XML::ATTR`
- `XML::CHILDREN`
- `XML::DECODE`
- `XML::DECODE_STREAM`
- `XML::ENCODE`

## Install

```sh
go get github.com/MontFerret/contrib/modules/xml
```

## Register The Module

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	xmlmodule "github.com/MontFerret/contrib/modules/xml"
)

func main() {
	xmlMod, err := xmlmodule.New()
	if err != nil {
		panic(err)
	}

	engine, err := ferret.New(
		ferret.WithModules(xmlMod),
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
| `XML::ROOT` | `XML::ROOT(value)` | `Object \| None` | Returns the root element for a document, the element itself, or `None` for text nodes. |
| `XML::TEXT` | `XML::TEXT(value)` | `String` | Concatenates descendant text in document order. |
| `XML::ATTR` | `XML::ATTR(value, name)` | `String \| None` | Returns an attribute value from a document root or element. |
| `XML::CHILDREN` | `XML::CHILDREN(value)` | `Array` | Returns element children, delegates through document roots, or `[]` for text nodes. |
| `XML::DECODE` | `XML::DECODE(data)` | `Object` | Decodes XML text into a normalized document object. |
| `XML::DECODE_STREAM` | `XML::DECODE_STREAM(data)` | `Iterator<Object>` | Streams normalized XML events. |
| `XML::ENCODE` | `XML::ENCODE(value)` | `String` | Encodes a normalized document or element object into XML text. |

## Decoded Shapes

Decoded documents use a stable, normalized object model:

```json
{
  "type": "document",
  "root": {
    "type": "element",
    "name": "book",
    "attrs": { "id": "123" },
    "children": [
      { "type": "text", "value": "hello" }
    ]
  }
}
```

`XML::DECODE_STREAM` yields event objects in this shape:

```json
{ "type": "startElement", "name": "book", "attrs": { "id": "123" } }
{ "type": "text", "value": "hello" }
{ "type": "endElement", "name": "book" }
```

## Examples

### Decode A Document

```fql
RETURN XML::DECODE("<book id=\"123\"><title>Hello</title></book>")
```

### Stream XML Events

```fql
FOR event IN XML::DECODE_STREAM("<book><title>Hello</title></book>")
RETURN event
```

### Access The Root Element

```fql
LET doc = XML::DECODE("<book id=\"123\"><title>Hello</title></book>")
RETURN XML::ROOT(doc)
```

### Read Descendant Text

```fql
LET doc = XML::DECODE("<book><title>Hello</title><subtitle>World</subtitle></book>")
RETURN XML::TEXT(doc)
```

### Read Attributes And Children

```fql
LET doc = XML::DECODE("<book id=\"123\"><title>Hello</title></book>")
LET root = XML::ROOT(doc)

RETURN {
  id: XML::ATTR(root, "id"),
  children: XML::CHILDREN(root)
}
```

### Encode A Normalized Element

```fql
RETURN XML::ENCODE({
  type: "element",
  name: "book",
  attrs: { id: "123" },
  children: [
    { type: "text", value: "Hello" }
  ]
})
```

## Behavior Notes

- `XML::ROOT`, `XML::TEXT`, `XML::ATTR`, and `XML::CHILDREN` operate on normalized XML document, element, and text nodes.
- `XML::ROOT(document)` returns the document root, `XML::ROOT(element)` returns the element, and `XML::ROOT(text)` returns `None`.
- `XML::TEXT` concatenates nested text nodes in document order and returns an empty string when no descendant text exists.
- `XML::ATTR` returns `None` for missing attributes and for text nodes.
- `XML::CHILDREN(text)` returns a fresh empty array.
- `XML::DECODE` and `XML::DECODE_STREAM` accept both string and binary XML input.
- Qualified names such as `ns:book` and `xmlns:ns` are preserved exactly.
- CDATA is normalized into `text` nodes or events.
- Comments, directives, and processing instructions are skipped in v1.
- Text inside elements is preserved exactly, including whitespace-only text nodes.
- Whitespace-only text outside the single root element is ignored.
- `XML::ENCODE` sorts attribute names for deterministic output because Ferret objects are unordered.
