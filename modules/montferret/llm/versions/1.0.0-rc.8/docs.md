# AI::LLM

`AI::LLM` exposes text generation, chat, summarization, structured extraction,
and classification to Ferret queries. Version 1 supports OpenAI through the
Responses API and keeps all conversation state local to the current Ferret run.

## Registration

Register the module when constructing the Ferret engine:

```go
engine, err := ferret.New(
	ferret.WithModules(llm.New()),
)
```

The module name is `ai/llm`, and its functions are registered under the
`AI::LLM` namespace.

## Quick start

Supply `openaiApiKey` as a Ferret execution parameter. Credentials are explicit:
the module does not read provider credentials from process environment
variables.

```fql
LET model = AI::LLM::MODEL("openai", {
  model: "gpt-5-mini",
  apiKey: @openaiApiKey
})

LET greeting = AI::LLM::GENERATE(
  model,
  "Write a one-sentence greeting for a new Ferret user."
)

LET session = AI::LLM::SESSION(model, {
  instructions: "Answer as a concise technical guide."
})
LET reply = AI::LLM::CHAT(session, "What can I do with AI::LLM?")

RETURN {
  greeting: greeting,
  reply: reply,
  history: AI::LLM::HISTORY(session)
}
```

## API reference

All functions use the `AI::LLM` namespace. Unknown option keys are rejected.
The operation functions accept either a stateless model or a local session as
their `target`:

- Functions: [`MODEL`](#model), [`SESSION`](#session),
  [`GENERATE`](#generate), [`CHAT`](#chat), [`SUMMARIZE`](#summarize),
  [`EXTRACT`](#extract), [`CLASSIFY`](#classify), [`RESET`](#reset),
  [`FORK`](#fork), and [`HISTORY`](#history).
- Other APIs: [Query API](#query-api), [Error reference](#error-reference),
  and [Go embedding API](#go-embedding-api).

- A **model** executes each request independently and has no conversation
  history.
- A **session** replays its existing history and commits the new input and
  assistant response only after a successful request.

### Shared value shapes

| Value | Shape or source | Notes |
|---|---|---|
| Model | Returned by `MODEL(..., {session: false})` | Stateless and safe for concurrent requests |
| Session | Returned by `MODEL(..., {session: true})`, `SESSION`, or `FORK` | Local to one Ferret run and serialized per session |
| Message | `{role: string, content: string}` | Role is `system`, `developer`, `user`, or `assistant` |
| Schema | JSON Schema object | Used by `EXTRACT`; see [Structured extraction schemas](#structured-extraction-schemas) |
| Labels | Nonempty array of unique, nonempty strings | Used by `CLASSIFY` |
| Classification | `{label: string}` | `label` is exactly one of the supplied labels |
| History | Array of `{role: string, content: string}` | Returned as a copy by `HISTORY` |

Models and sessions are opaque Ferret values. Their string, debug, and JSON
representations do not expose model names, API keys, history, or provider
configuration.

The Ferret SDK matches tagged fields in configuration objects and messages
case-insensitively. This reference uses the canonical spellings; JSON Schema
keywords inside schema documents remain case-sensitive.

### Shared execution options

`GENERATE`, `CHAT`, `SUMMARIZE`, `EXTRACT`, and `CLASSIFY` accept these keys in
their optional function options object. The same keys belong in `OPTIONS` when
using the [Query API](#query-api).

| Key | Type | Default when omitted | Validation and behavior |
|---|---|---|---|
| `temperature` | Number | Provider default | Must be between `0` and `2`; an explicit `0` is sent to the provider |
| `maxOutputTokens` | Integer | Provider default | Must be positive |
| `timeout` | Integer | `0` | Milliseconds; must be nonnegative; `0` adds no module deadline |

The temperature range is provider-neutral validation, not a guarantee that
every provider model accepts every value. Omit `temperature` to use the
provider default when model-specific support is unknown. When OpenAI identifies
`temperature` or `max_output_tokens` as an unsupported option, the operation
fails with `AI_LLM_INVALID_OPTIONS` and names the rejected public option.

### `MODEL`

```fql
AI::LLM::MODEL(provider, options)
```

Creates a provider-backed model handle.

| Argument | Type | Required | Description |
|---|---|---|---|
| `provider` | String | Yes | Provider identifier; trimmed and matched case-insensitively |
| `Config` | Object | Yes | Model configuration described below |

| Option | Type | Required | Default | Description |
|---|---|---|---|---|
| `model` | String | Yes | — | Nonblank provider model name; passed to the provider unchanged |
| `apiKey` | String | Yes | — | Nonblank explicit provider credential |
| `session` | Boolean | No | `false` | Return a default local session instead of a stateless model |

Returns a model when `session` is omitted or `false`. With `session: true`, it
returns the equivalent of creating a session with default session options.

```fql
LET model = AI::LLM::MODEL("openai", {
  model: "gpt-5-mini",
  apiKey: @openaiApiKey
})
RETURN model
```

Provider identifiers are case-insensitive. The module never reads credentials
from process environment variables and does not expose credentials through
value formatting, debugging, or JSON serialization.

### `SESSION`

```fql
AI::LLM::SESSION(model, options)
```

Creates a local conversation session from a stateless model. Passing an
existing session is rejected. The `Config` argument is required, but `{}` uses
all defaults.

| Argument | Type | Required | Description |
|---|---|---|---|
| `model` | Stateless model | Yes | Model returned by `MODEL` with `session: false` |
| `Config` | Object | Yes | Session configuration described below |

| Option | Type | Required | Default | Description |
|---|---|---|---|---|
| `instructions` | String | No | Empty | Persistent instructions included with every session request |
| `context` | Object | No | `{mode: "local", overflow: "error"}` | Local context metadata |

The `context` object accepts:

| Key | Type | Default | Validation and behavior |
|---|---|---|---|
| `mode` | String | `"local"` | Only `"local"` is supported |
| `overflow` | String | `"error"` | Only `"error"` is supported |
| `maxTokens` | Integer | `0` | Must be positive when specified; metadata only |
| `reserveOutputTokens` | Integer | `0` | Must be nonnegative and smaller than `maxTokens` when both are specified |

The module does not proactively count tokens. A configured token limit records
the intended model context metadata; provider context-window failures are
reported as `AI_LLM_CONTEXT_LIMIT_ERROR`.

```fql
LET session = AI::LLM::SESSION(model, {
  instructions: "Answer as a concise technical editor.",
  context: {
    mode: "local",
    overflow: "error",
    maxTokens: 128000,
    reserveOutputTokens: 4000
  }
})
RETURN session
```

See [Sessions](#sessions) for history, transaction, cleanup, and cancellation
behavior.

### `GENERATE`

```fql
AI::LLM::GENERATE(target, prompt[, options])
```

Generates text from one prompt.

| Argument | Type | Required | Description |
|---|---|---|---|
| `target` | Model or session | Yes | Provider model or local session |
| `prompt` | String | Yes | User prompt |
| `Config` | Object | No | `instructions` plus any [shared execution options](#shared-execution-options) |

`instructions` is an optional string sent separately from the user prompt.
Returns the generated text as a string. A successful call against a session
adds the prompt and assistant response to that session's history.

```fql
RETURN AI::LLM::GENERATE(model, "Name three uses for a web crawler.", {
  instructions: "Use a numbered list.",
  maxOutputTokens: 200
})
```

### `CHAT`

```fql
AI::LLM::CHAT(target, message[, options])
```

Generates an assistant response to a user message.

| Argument | Type | Required | Description |
|---|---|---|---|
| `target` | Model or session | Yes | Provider model or local session |
| `message` | String | Yes | Final user message for this request |
| `Config` | Object | No | `messages`, `instructions`, and shared execution options |

`messages` is an optional array of message objects:

```fql
[
  {role: "developer", content: "Prefer short answers."},
  {role: "user", content: "My project is called Ferret."},
  {role: "assistant", content: "Understood."}
]
```

Each message must contain exactly `role` and `content`. Supported roles are
`system`, `developer`, `user`, and `assistant`. The positional `message` is
always appended after this array as the final user message. `instructions` is
an optional string sent separately from the message list.

Returns the assistant response as a string. A successful session call commits
the supplied messages, final user message, and assistant response atomically.

```fql
RETURN AI::LLM::CHAT(model, "What is my project called?", {
  messages: [
    {role: "user", content: "My project is called Ferret."},
    {role: "assistant", content: "I will remember that for this request."}
  ]
})
```

### `SUMMARIZE`

```fql
AI::LLM::SUMMARIZE(target, text[, options])
```

Summarizes the supplied text and returns the summary as a string.

| Argument | Type | Required | Description |
|---|---|---|---|
| `target` | Model or session | Yes | Provider model or local session |
| `text` | String | Yes | Text to summarize |
| `Config` | Object | No | Options described below plus shared execution options |

| Option | Type | Validation and behavior |
|---|---|---|
| `style` | String | Requested summary style, such as `"bullet points"` |
| `maxWords` | Integer | Must be positive; requests a maximum word count |
| `instructions` | String | Additional summarization instructions |

```fql
RETURN AI::LLM::SUMMARIZE(model, article, {
  style: "bullet points",
  maxWords: 120,
  instructions: "Preserve product names.",
  maxOutputTokens: 600
})
```

### `EXTRACT`

```fql
AI::LLM::EXTRACT(target, text, schema[, options])
```

Extracts structured data from text, parses the provider response, validates it
against the supplied JSON Schema, and returns the matching Ferret value.

| Argument | Type | Required | Description |
|---|---|---|---|
| `target` | Model or session | Yes | Provider model or local session |
| `text` | String | Yes | Source text |
| `schema` | Object | Yes | JSON Schema supplied as the third positional argument |
| `Config` | Object | No | `instructions` plus shared execution options |

The schema cannot be placed inside the function options object. `instructions`
is an optional string containing additional extraction guidance. A successful
session call records the validated structured response as JSON text.

```fql
LET product = AI::LLM::EXTRACT(model, description, {
  type: "object",
  properties: {
    name: {type: "string"},
    price: {type: "number"}
  },
  required: ["name", "price"],
  additionalProperties: false
}, {
  instructions: "Use the advertised price."
})
RETURN product
```

See [Structured extraction schemas](#structured-extraction-schemas) for local
validation and OpenAI-specific schema restrictions.

### `CLASSIFY`

```fql
AI::LLM::CLASSIFY(target, text, labels[, options])
```

Selects exactly one label for the supplied text.

| Argument | Type | Required | Description |
|---|---|---|---|
| `target` | Model or session | Yes | Provider model or local session |
| `text` | String | Yes | Text to classify |
| `labels` | Array of strings | Yes | Nonempty, unique, nonempty allowed labels |
| `Config` | Object | No | `instructions` plus shared execution options |

The labels cannot be placed inside the function options object. Returns
`{label: "..."}`, where `label` is one of the supplied values.

```fql
RETURN AI::LLM::CLASSIFY(
  model,
  ticket,
  ["billing", "technical", "account"],
  {
    instructions: "Choose the primary support queue."
  }
)
```

### `RESET`

```fql
AI::LLM::RESET(session)
```

Clears all visible history from a local session and returns `true`. The
session's model and session options remain unchanged.

```fql
LET cleared = AI::LLM::RESET(session)
RETURN {cleared: cleared, history: AI::LLM::HISTORY(session)}
```

### `FORK`

```fql
AI::LLM::FORK(session)
```

Returns a new independent session with the same model, options, and copied
history. Later requests and resets on either session do not affect the other.
The fork is owned by the current Ferret run and is closed with the other
run-scoped sessions.

```fql
LET alternate = AI::LLM::FORK(session)
RETURN AI::LLM::CHAT(alternate, "Give me a different answer.")
```

### `HISTORY`

```fql
AI::LLM::HISTORY(session)
```

Returns a copied array of visible `{role, content}` messages. Reading or
modifying the returned array does not mutate the session.

```fql
RETURN AI::LLM::HISTORY(session)
```

## Structured extraction schemas

Extraction schemas are compiled before a provider request. Local fragment
references such as `#/$defs/address` are allowed, while external `$ref` values
are rejected. Provider output is parsed and validated locally before it is
returned or added to session history.

OpenAI structured output schemas are also checked against OpenAI's stricter
[Structured Outputs subset](https://developers.openai.com/api/docs/guides/structured-outputs)
before the request is sent. The root must be an object and cannot use `anyOf`;
every object must set
`additionalProperties: false`; and every declared property must appear in
`required`. `oneOf`, `allOf`, `not`, dependent schemas or requirements, and
conditional schemas are unsupported. Schemas may contain at most 10 levels of
object nesting, 5,000 properties, 1,000 enum values, and 120,000 total
characters across property names, definition names, string enum values, and
string constants. A string enum with more than 250 values is additionally
limited to 15,000 total characters. Incompatible schemas fail with
`AI_LLM_INVALID_SCHEMA` without contacting OpenAI. These restrictions are
OpenAI-specific and are not imposed on custom providers.

## Query API

Models and sessions implement Ferret's Queryable contract:

```fql
QUERY [ONE] expression IN target [USING mode]
  [WITH semanticOptions]
  [OPTIONS executionOptions]
```

- `expression` supplies the text processed by the selected operation.
- `target` is a model or session.
- `USING` selects the operation. Omitting it is equivalent to
  `USING generate`.
- `WITH` contains only operation-specific semantic data.
- `OPTIONS` contains only `temperature`, `maxOutputTokens`, and `timeout` from
  [Shared execution options](#shared-execution-options).

| `USING` mode | Expression meaning | Allowed `WITH` keys | Required `WITH` keys | Scalar result |
|---|---|---|---|---|
| Omitted or `generate` | User prompt | `instructions` | None | String |
| `chat` | Final user message | `messages`, `instructions` | None | String |
| `summarize` | Text to summarize | `style`, `maxWords`, `instructions` | None | String |
| `extract` | Text to inspect | `schema`, `instructions` | `schema` | Schema-matching Ferret value |
| `classify` | Text to classify | `labels`, `instructions` | `labels` | `{label: "..."}` |

Unlike the function API, Query places the extraction schema and classification
labels in `WITH`. Semantic keys are invalid in `OPTIONS`, and execution keys
are invalid in `WITH`.

### Text Query examples

```fql
LET model = AI::LLM::MODEL("openai", {
  model: "gpt-5-mini",
  apiKey: @openaiApiKey
})

LET generated = QUERY ONE "Write a haiku about parsers." IN model
LET summary = QUERY ONE article IN model USING summarize
  WITH {style: "concise", maxWords: 80}
  OPTIONS {maxOutputTokens: 300, timeout: 30000}

RETURN {generated: generated, summary: summary}
```

### Structured Query examples

```fql
LET product = QUERY ONE description IN model USING extract
  WITH {
    schema: {
      type: "object",
      properties: {
        name: {type: "string"},
        price: {type: "number"}
      },
      required: ["name", "price"],
      additionalProperties: false
    },
    instructions: "Use the advertised price."
  }
  OPTIONS {timeout: 30000}

LET category = QUERY ONE ticket IN model USING classify
  WITH {
    labels: ["billing", "technical", "account"],
    instructions: "Choose the primary support queue."
  }

RETURN {product: product, category: category}
```

Plain `QUERY` returns a singleton list because Ferret's queryable contract is
list-valued. Use `QUERY ONE` for the scalar result that matches the function
API. `QUERY COUNT` and `QUERY EXISTS` are deliberately unsupported because
either modifier would otherwise cause a potentially billable, state-mutating
provider request.

```fql
RETURN QUERY ONE email.body IN model USING summarize
```

## Sessions

`SESSION` accepts only a stateless model:

```fql
LET session = AI::LLM::SESSION(model, {
  instructions: "Answer as a concise technical editor.",
  context: {
    mode: "local",
    overflow: "error",
    maxTokens: 128000,
    reserveOutputTokens: 4000
  }
})

LET first = AI::LLM::CHAT(session, "My API returns widgets.")
LET second = AI::LLM::CHAT(session, "What does my API return?")
RETURN {
  first: first,
  second: second,
  history: AI::LLM::HISTORY(session)
}
```

Only local context with `overflow: "error"` is supported in version 1.
`maxTokens` and `reserveOutputTokens` are validated configuration metadata; the
module does not proactively count tokens. Provider context-window failures are
reported as `AI_LLM_CONTEXT_LIMIT_ERROR`.

Sessions replay visible text history on each request. They do not preserve
hidden reasoning, provider-private response items, tools, or server-side
conversation state. A successful call commits all input messages and the
assistant response atomically. Failed, refused, malformed, or schema-invalid
responses do not change history. Structured assistant responses appear in
history as their validated JSON text.

`RESET` clears history, `FORK` creates an independent copy, and `HISTORY`
returns a copy. Sessions created by `MODEL(..., {session: true})`, `SESSION`, or
`FORK` are scoped to the current Ferret execution and are closed after that run,
including sessions nested in arrays or objects.

## Error reference

AI::LLM errors have stable codes. Provider errors are sanitized and do not
expose credentials or raw provider response bodies.

| Code | Meaning |
|---|---|
| `AI_LLM_PROVIDER_ERROR` | Generic or unclassified provider failure |
| `AI_LLM_AUTH_ERROR` | Provider rejected the supplied credentials |
| `AI_LLM_RATE_LIMIT_ERROR` | Provider rate limit was reached |
| `AI_LLM_TIMEOUT_ERROR` | Configured deadline expired or the provider reported a timeout |
| `AI_LLM_CONTEXT_LIMIT_ERROR` | Request exceeded the provider's model context limit |
| `AI_LLM_UNSUPPORTED_PROVIDER` | No registered factory matches the provider identifier |
| `AI_LLM_UNSUPPORTED_OPERATION` | Target or Query mode does not support the requested operation |
| `AI_LLM_INVALID_OPTIONS` | Arguments or options are invalid, or OpenAI rejected a known execution option |
| `AI_LLM_INVALID_SCHEMA` | Extraction schema is missing, malformed, externally referenced, or unsupported by the provider |
| `AI_LLM_INVALID_STRUCTURED_OUTPUT` | Provider returned malformed or unusable structured output |
| `AI_LLM_SCHEMA_VALIDATION_ERROR` | Parsed structured output does not satisfy the requested schema |
| `AI_LLM_REFUSAL` | Provider explicitly refused the request |

For Go embedders, caller-initiated cancellation is returned as
`context.Canceled` rather than converted to a provider error. Configured
deadlines and provider HTTP 408 responses remain `AI_LLM_TIMEOUT_ERROR`.
Canceled session calls do not change history.

## Go embedding API

Normal registration with `llm.New()` creates an engine-owned registry,
registers the built-in OpenAI provider, and installs the registry into every
Ferret run context.

Install a custom provider factory as a module option:

```go
engine, err := ferret.New(
	ferret.WithModules(
		llm.New(
			llm.WithProviderFactory(factory),
		),
	),
)
```

Provider names are trimmed and matched case-insensitively. A custom factory
with the same normalized name as a built-in provider deliberately replaces the
built-in. When multiple module options provide the same name, the last factory
wins.

A provider factory implements:

```go
type ProviderFactory interface {
	Name() string
	NewModel(context.Context, core.ModelOptions) (core.Model, error)
}
```

The provider receives the model name and explicit API key through
`core.ModelOptions`. Model names remain opaque provider-specific strings.

### Registry APIs

| API | Behavior |
|---|---|
| `core.NewRegistry()` | Creates an empty provider registry |
| `registry.Register(factory)` | Adds a factory and rejects a duplicate normalized provider name |
| `registry.Replace(factory)` | Adds or replaces the factory under its normalized provider name |
| `registry.NewModel(ctx, provider, options)` | Resolves a provider and creates a model |
| `core.WithRegistry(ctx, registry)` | Returns a context carrying the registry; a nil parent context is replaced with `context.Background()` |
| `core.RegistryFrom(ctx)` | Returns the non-nil registry stored in the context |
| `core.ErrRegistryNotFound` | Sentinel returned when the context has no non-nil registry |

Custom integrations that invoke `core` or register `lib` directly can supply
their own context-carried registry:

```go
registry := core.NewRegistry()
if err := registry.Register(factory); err != nil {
	return err
}

ctx := core.WithRegistry(context.Background(), registry)
resolved, err := core.RegistryFrom(ctx)
```

The standard `llm.New()` module always installs its engine-owned registry for a
run, replacing any registry attached to the caller's original run context.

## Limitations

This initial module is text-only. Streaming, tools, agents, MCP, embeddings,
OCR, vision, vector search, persistent sessions, provider-side conversations,
custom base URLs, retries, and model discovery are outside its version 1
contract.
