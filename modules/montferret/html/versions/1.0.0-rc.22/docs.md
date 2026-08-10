# HTML Module

`github.com/MontFerret/contrib/modules/web/html` registers HTML loading, parsing, querying, interaction, and browser automation module functions for Ferret hosts.

The module exposes functions such as:

- `DOCUMENT`
- `PARSE`
- `ELEMENT`
- `ELEMENTS`
- `CLICK`
- `INPUT`
- `SELECT`
- `WAIT_ELEMENT`
- `SCREENSHOT`
- `PDF`

These are module functions and are an important part of the module's public surface. Query examples in this repository call them directly, for example `DOCUMENT(url)` and `ELEMENT(page, "#content")`.

## Install

```sh
go get github.com/MontFerret/contrib/modules/web/html
```

## Register The Module

Register the module with at least one driver. Most hosts should register the in-memory driver as the default and add the CDP driver for browser-backed pages.

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	htmlmodule "github.com/MontFerret/contrib/modules/web/html"
	"github.com/MontFerret/contrib/modules/web/html/drivers/cdp"
	"github.com/MontFerret/contrib/modules/web/html/drivers/memory"
)

func main() {
	htmlMod, err := htmlmodule.New(
		htmlmodule.WithDefaultDriver(memory.New()),
		htmlmodule.WithDrivers(
			cdp.New(cdp.WithAddress("http://localhost:9222")),
		),
	)
	if err != nil {
		panic(err)
	}

	engine, err := ferret.New(
		ferret.WithModules(htmlMod),
	)
	if err != nil {
		panic(err)
	}

	_ = engine
}
```

### Module Options

| Option | Notes |
| --- | --- |
| `WithDefaultDriver(driver)` | Registers `driver` and makes it the default for `DOCUMENT` and `PARSE` calls that do not specify `{ driver: "..." }`. |
| `WithDrivers(drivers...)` | Registers additional named drivers. |
| `WithNoLib()` | Registers the driver container hook without registering the FQL module functions. This is useful only for advanced embedding scenarios. |

## Drivers

The module ships with two driver implementations.

| Driver | Package | Name | Best For |
| --- | --- | --- | --- |
| In-memory HTTP/HTML | `drivers/memory` | `memory` | Fetching static HTML, parsing raw HTML, CSS/XPath queries, and in-memory DOM reads or mutations. |
| Chrome DevTools Protocol | `drivers/cdp` | `cdp` | JavaScript-rendered pages, browser interactions, navigation, scrolling, screenshots, PDFs, network events, and iframe-heavy pages. |

The CDP driver connects to an already-running browser endpoint. By default it uses `http://localhost:9222`.

One common local setup is to start Chrome or Chromium with remote debugging enabled:

```sh
chromium --headless=new --remote-debugging-port=9222 --remote-debugging-address=127.0.0.1 about:blank
```

Use the exact browser binary name available in your environment, such as `chromium`, `google-chrome`, or `Google Chrome`.

## Loading Pages

### Load Static HTML Over HTTP

When the default driver is `memory`, `DOCUMENT(url)` fetches and parses the response without launching a browser.

```fql
LET page = DOCUMENT("https://example.com")

RETURN {
  url: page.url,
  title: page.title,
  heading: ELEMENT(page, "h1").innerText
}
```

### Load A Browser-Backed Page

Use the `cdp` driver when the page needs JavaScript, real browser interaction, navigation waits, screenshots, or PDFs.

```fql
LET page = DOCUMENT("https://example.com/app", { driver: "cdp" })

WAIT_ELEMENT(page, "#app")

RETURN {
  url: page.url,
  title: page.title
}
```

### Parse Raw HTML

`PARSE` creates an HTML page from a string or binary value.

```fql
LET page = PARSE(`
  <html>
    <body>
      <article id="post">
        <h1>Hello</h1>
      </article>
    </body>
  </html>
`)

RETURN ELEMENT(page, "#post h1").innerText
```

### `DOCUMENT` Options

`DOCUMENT(url, params)` accepts a map with these keys.

| Option | Type | Notes |
| --- | --- | --- |
| `driver` | `String` | Driver name. Use `"memory"`, `"cdp"`, or another registered custom driver name. |
| `timeout` | `Int` | Page load timeout in milliseconds. |
| `userAgent` | `String` | User-Agent value for the request or browser page. |
| `keepCookies` | `Boolean` | Reuses browser/session cookies where the selected driver supports it. |
| `cookies` | `Object` or `Object[]` | Cookie or cookies to send during loading. |
| `headers` | `Object` | Request headers. |
| `viewport` | `Object` | Browser viewport options: `width`, `height`, `scaleFactor`, `mobile`, `landscape`. |
| `ignore.resources` | `Object[]` | Resource-blocking rules with `url` glob and optional `type`. |
| `ignore.statusCodes` | `Object[]` | HTTP status codes to allow, optionally scoped by `url` glob. |
| `charset` | `String` | Source charset to convert to UTF-8. Applies to the memory driver. |
| `initScript` | `Object` | CDP-only script with required `source` and optional `timing`: `afterNavigation` (default) or `beforeDocument`. |

```fql
LET page = DOCUMENT("https://example.com/dashboard", {
  driver: "cdp",
  timeout: 10000,
  userAgent: "FerretBot/1.0",
  viewport: {
    width: 1280,
    height: 720
  },
  headers: {
    "X-Trace": "docs"
  },
  initScript: {
    source: "window.__ferretReady = true",
    timing: "beforeDocument"
  },
  cookies: [
    {
      name: "session",
      value: "abc123",
      domain: "example.com",
      path: "/",
      httpOnly: true,
      secure: true
    }
  ],
  ignore: {
    resources: [
      { url: "*.png", type: "Image" }
    ],
    statusCodes: [
      { url: "https://example.com/optional/*", code: 404 }
    ]
  }
})

RETURN page.url
```

`DOCUMENT(url, "cdp")` is also supported as a shorthand for selecting a driver by name.

`afterNavigation` evaluates the script after each successful CDP-controlled initial,
explicit, back, or forward navigation reaches Ferret's main-frame readiness point.
`beforeDocument` uses the browser's new-document mechanism before the first real
navigation, so same-target frames can observe it from their earliest page code,
subject to browser target limitations, and it remains installed across redirects
and reloads. The memory driver rejects `initScript`.

### `PARSE` Options

`PARSE(html, params)` accepts `driver`, `keepCookies`, `cookies`, `headers`, and `viewport`.

```fql
LET page = PARSE("<html><body><h1>Loaded</h1></body></html>", {
  driver: "memory"
})

RETURN ELEMENT(page, "h1").innerText
```

## Querying

Use CSS selectors by default.

```fql
LET page = DOCUMENT($url)
LET item = ELEMENT(page, ".product")

RETURN {
  title: ELEMENT(item, ".title").innerText,
  price: ELEMENT(item, ".price").innerText,
  count: ELEMENTS_COUNT(page, ".product")
}
```

Use `ELEMENTS` for all matches and `ELEMENT_EXISTS` to test a selector without failing a flow.

```fql
LET page = DOCUMENT($url)

FOR link IN ELEMENTS(page, "a[href]")
  RETURN {
    text: link.innerText,
    href: link.attributes.href
  }
```

XPath selectors can be passed directly with `X(...)` or evaluated through `XPATH(...)`.

```fql
LET page = DOCUMENT($url)

RETURN {
  first: ELEMENT(page, X("//main//h1")).innerText,
  raw: XPATH(page, "//a/@href")
}
```

Query module functions accept `HTMLPage`, `HTMLDocument`, and `HTMLElement` roots where the underlying function supports root targets. This makes it practical to narrow a query step by step:

```fql
LET page = DOCUMENT($url)
LET main = ELEMENT(page, "main")

RETURN ELEMENTS(main, "article")
```

### Ferret v2 Query Syntax

The function-backed query style above is still supported. Ferret v2 also provides query expressions that work directly with queryable HTML values.

```fql
LET page = DOCUMENT($url)

LET first = QUERY ONE ".product" IN page USING css
LET count = QUERY COUNT ".product" IN page USING css
LET hasFeatured = QUERY EXISTS ".product.featured" IN page USING css

RETURN {
  title: (QUERY VALUE ".title" IN first USING css).innerText,
  count: count,
  hasFeatured: hasFeatured
}
```

Use `QUERY VALUE` when a missing result should be an error, `QUERY ONE` when exactly one result is required, `QUERY ANY` when the first result is enough, `QUERY EXISTS` for booleans, and `QUERY COUNT` for counts.

`QUERY ... IN . USING css` is useful inside projections where `.` is the current value:

```fql
LET page = DOCUMENT($url)
LET sections = QUERY ".section" IN page USING css

RETURN sections[* RETURN {
  heading: (QUERY VALUE "h2" IN . USING css).innerText,
  links: QUERY "a" IN . USING css
}]
```

### CSSX Selection Pipelines

CSS queries can use CSSX pseudo-functions to transform and refine their results. Pseudo-functions operate on a normalized selection, so singular projection names such as `:text` and `:attr` apply to every selected item.

```fql
LET page = DOCUMENT($url)

LET paragraphs = QUERY ":text(p)" IN page USING css
LET links = QUERY 'a >> :attr("href") >> :compact() >> :distinct()' IN page USING css
LET cards = QUERY '.card >> :has(".price") >> :children(".title") >> :text()' IN page USING css

RETURN {
  paragraphs: paragraphs,
  links: links,
  firstParagraph: QUERY ONE ":text(p)" IN page USING css,
  cards: cards
}
```

`QUERY` returns the complete final selection. `QUERY ONE` evaluates the same pipeline and returns its first final value. `QUERY COUNT` and `QUERY EXISTS` inspect the final result shape.

CSSX operation families:

| Family | Operations | Behavior |
| --- | --- | --- |
| Maps | `text`, `ownText`, `normalize`, `trim`, `attr`, `prop`, `html`, `outerHtml`, `value`, `absUrl`, `url`, `parseUrl`, `replace`, `regex`, `toNumber`, `toDate` | Return one value per input slot and preserve missing values as `NONE`. |
| Traversals | `parent`, `closest`, `children`, `next`, `prev`, `siblings` | Flat-map nodes in input order, preserve duplicates, and omit missing traversal results. |
| Filters | `within`, `has`, `matches`, `not`, `withAttr`, `withText` | Keep matching nodes from the input selection. |
| Selection operators | `take`, `skip`, `slice`, `compact`, `distinct`, `dedupeByAttr`, `dedupeByText` | Return another selection. `compact` removes `NONE`; `distinct` performs stable identity/value deduplication. |
| Reducers | `exists`, `empty`, `count`, `one`, `indexOf`, `len`, `join` | Collapse the selection to one value. |
| Cardinality | `first`, `last`, `nth` | Collapse to one item or `NONE`; a following ordinary pseudo-function lifts the item into a selection again. |

Predicates and filtered traversals take a literal CSS criterion evaluated relative to each input node:

```fql
LET priced = QUERY '.product >> :has(".price")' IN page USING css
LET inactive = QUERY 'li >> :not(".active")' IN page USING css
LET containers = QUERY ':closest(".card", .title)' IN page USING css
```

Mapped `NONE` values keep their positions and count toward `count`, `exists`, `empty`, and `one`. Use `:compact()` when missing values should be removed before a reducer.

## Reading And Mutating DOM Content

HTML page, document, and element values expose a dot-access surface for convenient reads. Static/memory-backed values are read-only through dot access. CDP-backed elements also support a small write-through assignment surface.

```fql
LET page = DOCUMENT($url)
LET input = ELEMENT(page, "input[name=q]")

RETURN {
  url: page.url,
  document: page.document,
  title: page.title,
  value: input.value,
  text: input.innerText,
  html: input.innerHTML,
  attrs: input.attributes,
  styles: input.style
}
```

Common readable properties include:

| Value | Properties |
| --- | --- |
| `HTMLPage` | `response`, `mainFrame`, `document`, `frames`, `url`, `URL`, `cookies`, `title`, `isClosed`, plus document properties through the main frame. |
| `HTMLDocument` | `url`, `URL`, `name`, `title`, `parent`, `body`, `head`, `innerHTML`, `innerText`, plus node properties. |
| `HTMLElement` | `innerText`, `innerHTML`, `textContent`, `value`, `checked` (CDP), `disabled` (CDP), `selected` (CDP), `attributes`, `style`, `classes` (CDP), `dataset` (CDP), `previousElementSibling`, `nextElementSibling`, `parentElement`, plus node properties. |
| HTML node values | integer child indexes, `nodeType`, `nodeName`, `children`, `length`. |

Use the mutation module functions for driver-portable writes:

```fql
LET page = DOCUMENT($url, { driver: "cdp" })

INNER_TEXT_SET(page, "#status", "Ready")
INNER_HTML_SET(page, "#preview", "<strong>Ready</strong>")
LET preview = ELEMENT(page, "#preview")
ATTR_SET(preview, "data-state", "ready")
STYLE_SET(preview, "display", "block")

RETURN preview.innerHTML
```

CDP-backed elements can also be mutated with normal assignment. Top-level assignment supports content/value properties plus `attributes`, `style`, `classes`, and `dataset`; nested assignment writes through snapshot views returned by those collection properties. A captured view keeps its read snapshot while writes through that view update the browser:

```fql
LET page = DOCUMENT($url, { driver: "cdp" })
LET button = ELEMENT(page, "button[type=submit]")

button.textContent = "Continue"
button.innerHTML = "<strong>Continue</strong>"
button.disabled = FALSE
button.attributes = { "aria-label": "Continue" }
button.attributes["data-state"] = "ready"
button.style.display = "block"
button.classes.active = TRUE
button.dataset.productId = "123"

button.attributes["data-state"] = NONE
button.style.display = NONE
button.classes.active = FALSE
button.dataset.productId = NONE
```

Attribute and style module functions can work with individual names or maps depending on the function:

```fql
LET page = DOCUMENT($url)
LET card = ELEMENT(page, ".card")

RETURN {
  attrs: ATTR_GET(card, "id", "class", "data-kind"),
  matching: ATTR_QUERY(page, ".card", "data-kind"),
  styles: STYLE_GET(card, "display", "color")
}
```

## Browser Interaction

Browser-style interaction requires a driver that supports interaction capabilities. In practice, use the CDP driver for user-like workflows.

```fql
LET page = DOCUMENT($url, { driver: "cdp" })

WAIT_ELEMENT(page, "form")
INPUT(page, "input[name=email]", "user@example.com")
INPUT(page, "input[name=password]", "secret")
CLICK(page, "button[type=submit]")
WAIT_NAVIGATION(page, { timeout: 10000 })

RETURN page.url
```

`SELECT` supports both single and multiple values, matching the browser `<select>` element behavior.

```fql
LET page = DOCUMENT($url, { driver: "cdp" })

WAIT_ELEMENT(page, "#multi_select_input")

RETURN SELECT(page, "#multi_select_input", ["1", "2", "4"])
```

Other interaction module functions include:

```fql
FOCUS(page, "input[name=q]")
PRESS(ELEMENT(page, "input[name=q]"), "Enter")
PRESS_SELECTOR(page, "input[name=q]", "Meta+A")
INPUT_CLEAR(page, "input[name=q]")
HOVER(page, ".menu")
UNHOVER(page, ".menu")
BLUR(page, "input[name=q]")
MOUSE(page, 100, 200)
```

### Ferret v2 Dispatch Syntax

Ferret v2 supports both long-form dispatch and receiver-first shorthand dispatch for values that implement Ferret's dispatcher contract:

```fql
DISPATCH "click" IN target
target <- "click"
```

With the CDP driver, `HTMLPage`, `HTMLDocument`, and `HTMLElement` values support browser-backed dispatch actions. `WITH { ... }` is the action payload, and dispatch `OPTIONS` are reserved.

```fql
DISPATCH "click" IN button
DISPATCH "input" IN searchBox WITH { value: "macbook" }
DISPATCH "select" IN countrySelect WITH { value: "US" }
DISPATCH "keydown" IN input WITH { key: "Enter" }
DISPATCH "type" IN input WITH { text: "macbook pro", delay: 50 }
DISPATCH "scroll" IN doc WITH { y: 1200 }
DISPATCH "scroll" IN doc WITH { to: "bottom" }
DISPATCH "scroll" IN container WITH { by: { y: 800 } }
DISPATCH "scroll" IN item WITH { intoView: true }
```

Supported CDP dispatch event names are:

| Category | Events |
| --- | --- |
| Mouse | `click`, `dblclick`, `mousedown`, `mouseup`, `mouseover`, `mouseout`, `mousemove` |
| Keyboard | `keydown`, `keyup`, `keypress`, `press`, `type` |
| Forms | `input`, `change`, `submit`, `reset`, `focus`, `blur`, `check`, `uncheck`, `toggle`, `select` |
| Scroll | `scroll` |

Mouse payloads may include `button`, `count`, `x`, and `y`. Keyboard `press` accepts `key` or `keys`; `type` accepts `text`, optional `delay`, and optional `clear`. Form `input` and `select` require `value`; `change` may include `value`. Scroll accepts absolute coordinates via `x`/`y` or `to`, named absolute targets via `to: "top"` or `to: "bottom"`, relative coordinates via `by`, or `intoView: true`.

Navigation and scrolling module functions:

```fql
NAVIGATE(page, "https://example.com/next")
NAVIGATE_BACK(page)
NAVIGATE_FORWARD(page)
SCROLL(page, 0, 400)
SCROLL_TOP(page)
SCROLL_BOTTOM(page)
SCROLL_ELEMENT(page, "#target")
```

## Waiting

Wait module functions suspend execution until a condition is met or the current context times out.

```fql
LET page = DOCUMENT($url, { driver: "cdp" })

WAIT_ELEMENT(page, "#loaded")
WAIT_CLASS(page, "#status", "ready")
WAIT_ATTR(page, "#status", "data-state", "ready")
WAIT_STYLE(page, "#status", "display", "block")

RETURN INNER_TEXT(page, "#status")
```

Each condition has a negative form and an all-matches form:

| Positive | Negative | All-Matches Positive | All-Matches Negative |
| --- | --- | --- | --- |
| `WAIT_ELEMENT` | `WAIT_NO_ELEMENT` | | |
| `WAIT_ATTR` | `WAIT_NO_ATTR` | `WAIT_ATTR_ALL` | `WAIT_NO_ATTR_ALL` |
| `WAIT_CLASS` | `WAIT_NO_CLASS` | `WAIT_CLASS_ALL` | `WAIT_NO_CLASS_ALL` |
| `WAIT_STYLE` | `WAIT_NO_STYLE` | `WAIT_STYLE_ALL` | `WAIT_NO_STYLE_ALL` |

Use `WAIT_NAVIGATION(page, params?)` for browser navigation. It accepts either a timeout integer or a parameter map with `timeout`, `target`, and `frame`.

```fql
CLICK(page, "a.next")
WAIT_NAVIGATION(page, {
  target: "https://example.com/next",
  timeout: 10000
})
```

### Ferret v2 `WAITFOR` Syntax

The function-backed wait style above is useful for common DOM conditions. Ferret v2 also provides `WAITFOR VALUE` for polling any expression until it becomes meaningful.

```fql
LET page = DOCUMENT($url, { driver: "cdp" })

LET first = WAITFOR VALUE (QUERY ONE ".track[data-index='0']" IN page USING css)
  TIMEOUT 10s
  EVERY 250ms
  ON TIMEOUT RETURN NONE

RETURN first == NONE ? NONE : first.innerText
```

For browser-backed pages, `WAITFOR EVENT` subscribes to page, network, navigation, document, or element events.

```fql
LET page = DOCUMENT($url, { driver: "cdp" })

INPUT(page, "#url", "https://example.com/next")
CLICK(page, "#submit")

WAITFOR EVENT "navigation" IN page

RETURN page.url
```

`WAITFOR EVENT` can filter events with `WHEN`, configure event-specific options, and recover from timeouts:

```fql
LET page = DOCUMENT($url, { driver: "cdp" })

CLICK(page, "#load-data")

LET evt = WAITFOR EVENT "network.request_finished" IN page
  OPTIONS { captureBody: true, bodyLimit: 4096 }
  WHEN CONTAINS(.url, "/api/data")
  AND .status == 200
  AND .type == "fetch"
  TIMEOUT 10s
  ON TIMEOUT RETURN NONE

RETURN evt == NONE ? NONE : {
  status: evt.status,
  body: evt.body,
  truncated: evt.bodyTruncated
}
```

DOM custom events can be observed from documents or elements:

```fql
LET page = DOCUMENT($url, { driver: "cdp" })
LET target = ELEMENT(page, "#observable-element-target")

CLICK(page, "#observable-element-btn")

LET evt = WAITFOR EVENT "ferret:element" IN target
  WHEN .detail.scope == "element"

RETURN evt.detail
```

## Cookies, Headers, Frames, And Page Artifacts

Page cookies can be read through `page.cookies` or through `COOKIE_GET`, and can be changed with `COOKIE_SET` and `COOKIE_DEL` where the selected driver supports page cookies.

```fql
LET page = DOCUMENT($url, {
  driver: "cdp",
  cookies: {
    name: "mode",
    value: "docs",
    domain: "example.com",
    path: "/"
  }
})

COOKIE_SET(page, {
  name: "seen",
  value: "true",
  domain: "example.com",
  path: "/"
})

RETURN COOKIE_GET(page, "seen")
```

Frame module functions read the current page frame tree.

```fql
LET page = DOCUMENT($url, { driver: "cdp" })

WAIT_ELEMENT(page, "iframe")

FOR frame IN FRAMES(page, 0, 10)
  RETURN {
    name: frame.name,
    url: frame.url
  }
```

Screenshots and PDFs return binary values.

```fql
LET page = DOCUMENT($url, { driver: "cdp" })

RETURN {
  screenshot: SCREENSHOT(page, {
    format: "png",
    width: 1280,
    height: 720
  }),
  pdf: PDF(page, {
    printBackground: true,
    preferCSSPageSize: true
  })
}
```

`SCREENSHOT` and `PDF` also accept a URL string as the target. In that form the function opens the page and closes it after capturing the artifact.

## Function Reference

### Loading And Type Checks

| Function | Signature | Returns | Notes |
| --- | --- | --- | --- |
| `DOCUMENT` | `DOCUMENT(url, params?)` | `HTMLPage` | Loads a URL through the default or named driver. |
| `DOCUMENT_EXISTS` | `DOCUMENT_EXISTS(url, options?)` | `Boolean` | Checks whether a document can be loaded. Supports request headers. |
| `PARSE` | `PARSE(htmlOrBinary, params?)` | `HTMLPage` | Parses raw HTML content into an HTML page. |
| `IS_HTML_DOCUMENT` | `IS_HTML_DOCUMENT(value)` | `Boolean` | Reports whether a value is an `HTMLDocument`. |
| `IS_HTML_ELEMENT` | `IS_HTML_ELEMENT(value)` | `Boolean` | Reports whether a value is an `HTMLElement`. |

### Querying

| Function | Signature | Returns | Notes |
| --- | --- | --- | --- |
| `ELEMENT` | `ELEMENT(root, selector)` | `HTMLElement \| None` | Returns the first matching element. |
| `ELEMENTS` | `ELEMENTS(root, selector)` | `HTMLElement[]` | Returns all matching elements. |
| `ELEMENT_EXISTS` | `ELEMENT_EXISTS(root, selector)` | `Boolean` | Reports whether at least one match exists. |
| `ELEMENTS_COUNT` | `ELEMENTS_COUNT(root, selector)` | `Int` | Counts matching elements. |
| `X` | `X(expression)` | `QuerySelector` | Builds an XPath selector value. |
| `XPATH` | `XPATH(root, expression)` | `Any` | Evaluates an XPath expression. |

### Content, Attributes, And Styles

| Function | Signature | Returns | Notes |
| --- | --- | --- | --- |
| `INNER_TEXT` | `INNER_TEXT(root, selector?)` | `String` | Reads text from a root or selected element. |
| `INNER_TEXT_ALL` | `INNER_TEXT_ALL(root, selector)` | `String[]` | Reads text from every matching element. |
| `INNER_TEXT_SET` | `INNER_TEXT_SET(root, selector?, value)` | `None` | Sets text on a root or selected element. |
| `INNER_HTML` | `INNER_HTML(root, selector?)` | `String` | Reads HTML from a root or selected element. |
| `INNER_HTML_ALL` | `INNER_HTML_ALL(root, selector)` | `String[]` | Reads HTML from every matching element. |
| `INNER_HTML_SET` | `INNER_HTML_SET(root, selector?, value)` | `None` | Sets HTML on a root or selected element. |
| `ATTR_GET` | `ATTR_GET(root, name...)` | `Object` | Reads selected attributes. |
| `ATTR_QUERY` | `ATTR_QUERY(root, selector, name...)` | `Object` | Reads selected attributes from the first matching element. |
| `ATTR_SET` | `ATTR_SET(root, nameOrMap, value?)` | `None` | Sets one or more attributes. |
| `ATTR_REMOVE` | `ATTR_REMOVE(root, name...)` | `None` | Removes attributes. |
| `STYLE_GET` | `STYLE_GET(element, name...)` | `Object` | Reads selected style values. |
| `STYLE_SET` | `STYLE_SET(element, nameOrMap, value?)` | `None` | Sets one or more style values. |
| `STYLE_REMOVE` | `STYLE_REMOVE(element, name...)` | `None` | Removes style values. |

### Interaction

| Function | Signature | Returns | Notes |
| --- | --- | --- | --- |
| `CLICK` | `CLICK(root, selectorOrCount?, count?)` | `Boolean` | Clicks a root or selected element. |
| `CLICK_ALL` | `CLICK_ALL(root, selector, count?)` | `Boolean` | Clicks every matching element. |
| `INPUT` | `INPUT(root, selector?, value, delay?)` | `Boolean` | Types into a root or selected input target. |
| `INPUT_CLEAR` | `INPUT_CLEAR(root, selector?)` | `Boolean` | Clears a root or selected input target. |
| `PRESS` | `PRESS(root, keys, count?)` | `Boolean` | Sends keyboard input to the root target. |
| `PRESS_SELECTOR` | `PRESS_SELECTOR(root, selector, keys, count?)` | `Boolean` | Sends keyboard input to a selected element. |
| `SELECT` | `SELECT(root, selector?, valueOrValues)` | `String[]` | Selects values in a `<select>` element. |
| `FOCUS` | `FOCUS(root, selector?)` | `Boolean` | Focuses a root or selected element. |
| `BLUR` | `BLUR(root, selector?)` | `Boolean` | Blurs a root or selected element. |
| `HOVER` | `HOVER(root, selector?)` | `Boolean` | Hovers a root or selected element. |
| `UNHOVER` | `UNHOVER(root, selector?)` | `Boolean` | Moves the mouse outside a root or selected element using a randomized offset. |
| `MOUSE` | `MOUSE(pageOrDocument, x, y)` | `Boolean` | Moves the mouse to absolute viewport coordinates; returns false when it cannot move further or is already at the target. |

### Navigation, Scrolling, And Waiting

| Function | Signature | Returns | Notes |
| --- | --- | --- | --- |
| `NAVIGATE` | `NAVIGATE(page, url)` | `Boolean` | Navigates the page. |
| `NAVIGATE_BACK` | `NAVIGATE_BACK(page, skip?)` | `Boolean` | Navigates backward in browser history. |
| `NAVIGATE_FORWARD` | `NAVIGATE_FORWARD(page, skip?)` | `Boolean` | Navigates forward in browser history. |
| `WAIT_NAVIGATION` | `WAIT_NAVIGATION(page, paramsOrTimeout?)` | `Boolean` | Waits for page or frame navigation. |
| `SCROLL` | `SCROLL(root, x, y, options?)` | `Boolean` | Scrolls to absolute viewport coordinates; returns false when already at the effective target. |
| `SCROLL_TOP` | `SCROLL_TOP(root, options?)` | `Boolean` | Scrolls to the top; returns false when already there. |
| `SCROLL_BOTTOM` | `SCROLL_BOTTOM(root, options?)` | `Boolean` | Scrolls to the bottom; returns false when already there. |
| `SCROLL_ELEMENT` | `SCROLL_ELEMENT(root, selectorOrOptions?, options?)` | `Boolean` | Scrolls an element into view; selector-based calls return false when it is already fully visible. |
| `WAIT_ELEMENT` / `WAIT_NO_ELEMENT` | `WAIT_ELEMENT(root, selector)` | `Boolean` | Waits for element presence or absence. |
| `WAIT_ATTR` / `WAIT_NO_ATTR` | `WAIT_ATTR(root, selector?, name, value)` | `Boolean` | Waits for attribute state. |
| `WAIT_ATTR_ALL` / `WAIT_NO_ATTR_ALL` | `WAIT_ATTR_ALL(root, selector, name, value)` | `Boolean` | Waits for all matching elements. |
| `WAIT_CLASS` / `WAIT_NO_CLASS` | `WAIT_CLASS(root, selector?, class)` | `Boolean` | Waits for class state. |
| `WAIT_CLASS_ALL` / `WAIT_NO_CLASS_ALL` | `WAIT_CLASS_ALL(root, selector, class)` | `Boolean` | Waits for all matching elements. |
| `WAIT_STYLE` / `WAIT_NO_STYLE` | `WAIT_STYLE(root, selector?, name, value)` | `Boolean` | Waits for style state. |
| `WAIT_STYLE_ALL` / `WAIT_NO_STYLE_ALL` | `WAIT_STYLE_ALL(root, selector, name, value)` | `Boolean` | Waits for all matching elements. |

### Page Data And Artifacts

| Function | Signature | Returns | Notes |
| --- | --- | --- | --- |
| `URL` | `URL(pageOrDocument)` | `String` | Returns the current document URL. |
| `BASE_URL` | `BASE_URL(pageOrDocument)` | `String` | Returns the effective document base URL, including `<base>`. |
| `RESOLVE_URL` | `RESOLVE_URL(pageOrDocument, url)` | `String` | Resolves a URL against the effective document base URL. |
| `COOKIE_GET` | `COOKIE_GET(page, name)` | `HTTPCookie \| None` | Reads a page cookie by name. |
| `COOKIE_SET` | `COOKIE_SET(page, cookieOrCookies...)` | `None` | Sets page cookies. |
| `COOKIE_DEL` | `COOKIE_DEL(page, cookieOrNames...)` | `None` | Deletes page cookies. |
| `FRAMES` | `FRAMES(page, offset, count)` | `HTMLDocument[]` | Returns a slice of page frames. |
| `SCREENSHOT` | `SCREENSHOT(pageOrUrl, params?)` | `Binary` | Captures a screenshot. |
| `PDF` | `PDF(pageOrUrl, params?)` | `Binary` | Prints the page to PDF. |
| `DOWNLOAD` | `DOWNLOAD(url)` | `Binary` | Downloads a resource by URL. |
| `PAGINATION` | `PAGINATION(page, selector)` | `Iterator<Int>` | Iterates through pages by clicking a next-page selector. |

## Behavior Notes

- `DOCUMENT` uses the default registered driver unless the second argument selects a driver by string or by `{ driver: "..." }`.
- The memory driver is suitable for static documents and tests that do not require a live browser.
- The CDP driver requires a reachable Chrome DevTools endpoint before query execution starts.
- Browser-backed module functions operate on live page state. Use wait module functions or `WAITFOR` before reading or interacting with elements that are rendered asynchronously.
- HTML page, document, and element dot access is read-only. Use explicit module functions such as `ATTR_SET`, `STYLE_SET`, `INNER_TEXT_SET`, `INPUT`, and `CLICK` for effects.
- Unknown HTML dot properties return `None`.
- `page.frames` includes the root document and child frames.
- `DOCUMENT_EXISTS` is intended for availability checks; use `DOCUMENT` when you need the resulting page value.
- Cookie maps require at least `name` and `value`. Common optional fields are `path`, `domain`, `maxAge`, `expires`, `sameSite`, `httpOnly`, and `secure`.

## Contributing

This repository is a workspace of independently versioned modules. Keep `web/html` changes inside the owning module unless the task is explicitly about repo-wide tooling or CI.

### Package Orientation

| Path | Owns |
| --- | --- |
| `modules/web/html` | Module construction and registration options. |
| `modules/web/html/lib` | Public FQL module function registration and argument adaptation. |
| `modules/web/html/drivers` | Shared driver contracts, runtime value types, selector types, cookies, headers, screenshots, PDFs, and capability adapters. |
| `modules/web/html/drivers/memory` | Static HTTP loading and in-memory DOM implementation. |
| `modules/web/html/drivers/cdp` | Browser-backed implementation using Chrome DevTools Protocol. |
| `modules/web/html/drivers/cdp/dom` | CDP DOM documents, elements, frame loading, and DOM subscriptions. |
| `modules/web/html/drivers/cdp/input` | Mouse, keyboard, focus, form input, select, and scroll interaction. |
| `modules/web/html/drivers/cdp/network` | Network events, cookies, request/response observation, and navigation-related streams. |
| `modules/web/html/drivers/cdp/templates` | JavaScript snippets evaluated in the browser. |
| `tests/modules/web/html` | Ferret Lab integration fixtures for static and dynamic pages. |
| `tests/data/pages/static` and `tests/data/pages/dynamic` | Shared pages served by integration tests. |

### Development Workflow

1. Identify whether the behavior belongs to `lib`, shared `drivers`, `drivers/memory`, or `drivers/cdp`.
2. Inspect existing unit tests and Lab fixtures before adding new coverage.
3. Keep driver-specific behavior in the owning driver package.
4. Add focused unit tests for module-local logic changes.
5. Add or update `tests/modules/web/html` fixtures for user-visible FQL behavior.
6. Update this README when public module function behavior, options, driver contracts, or contribution workflow changes.

Use the repo-level Makefile from the repository root.

```sh
make test-unit web/html
make build web/html
make test-integration web/html
make lint web/html
make fmt web/html
```

`make test-unit web/html` is the narrow first validation for module code. `make build web/html` rebuilds the test runtime used by Lab integration tests. `make test-integration web/html` expects a CDP browser to be reachable at `http://127.0.0.1:9222/json/version`; if no browser is running, the command cannot validate browser-backed fixtures.

For documentation-only changes, a Markdown review is enough. Do not claim Go tests or integration tests were run unless they were actually executed.

### Design Rules For Contributors

- Do not move HTML behavior into the repository root or another module without a clear ownership reason.
- Do not assume Ferret core compiler, parser, VM, or runtime internals live in this repository.
- Prefer local module patterns over broad new abstractions.
- Preserve driver boundaries: static DOM behavior belongs in `drivers/memory`, browser behavior belongs in `drivers/cdp`, and shared contracts belong in `drivers`.
- Keep public FQL behavior documented here and covered by focused tests or fixtures.
- Avoid hidden cross-module dependencies.
