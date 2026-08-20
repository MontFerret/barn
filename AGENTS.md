# AGENTS.md

This file is the canonical operating guide for coding agents working in this repository. It is written for the current Barn Registry implementation. If repository documentation conflicts with this file, prefer `Makefile`, `go.mod`, `.github/workflows/ci.yml`, `.github/workflows/stamp-publications.yml`, and the current tests for commands, toolchains, CI behavior, and executable contracts.

## Repo snapshot

* Module path: `github.com/MontFerret/barn`
* Minimum Go version in `go.mod`: Go 1.25
* CI and pinned-release analysis toolchain: Go 1.26.x
* Barn is the Git-backed Ferret Registry.
* Human-reviewed source records live under `registry/`.
* The public static distribution is persisted on the CI-owned `gh-pages` branch and published from its root.
* Ignored `dist/` remains disposable full-generation output for local recovery and debugging.
* Barn consumes Registry v1, Module Manifest v1, and Registry artifact v1 contracts from `github.com/MontFerret/specs`.

Do not infer Barn's current behavior from historical Registry layouts, old release procedures, or consumer-side path construction. The current source tree, generated artifact links, public packages, tests, and workflows are authoritative.

## Architectural mental model

Barn has two related flows: Registry validation/generation and release preparation.

Registry validation and generation:

```text
registry source records
    -> strict source loading and identity validation
    -> changed-release selection from gh-pages source provenance
    -> exact tag/commit resolution and enrichment for affected releases
    -> byte-for-byte reuse of unchanged immutable release artifacts
    -> cheap module, category, plugin, and root projections
    -> deterministic Registry artifact documents
    -> validated candidate committed atomically to gh-pages
```

Release preparation:

```text
local module + pushed tag
    -> ferret.yaml parsing and Registry lookup
    -> public tag/commit and pinned-source inspection
    -> new-module or new-version classification
    -> deterministic Barn-relative source records
    -> caller-owned branch/commit/pull-request transport
```

Agents should reason about changes by ownership boundary:

* Specs owns portable source schemas, generated artifact schemas, strict parsing, and same-document validation.
* Barn's internal packages own Registry source discovery, Git and filesystem inspection, cross-record behavior, API extraction, documentation rendering, distribution projection, immutability, and publication stamping.
* `pkg/registry` owns HTTP transport, same-origin link navigation, cross-document checks, and consumer-facing projections for the generated static Registry.
* `pkg/publish` owns non-mutating release validation and deterministic source-record preparation.
* The Ferret CLI or another caller owns credentials, forks, branches, commits, hosting APIs, retries, and pull-request submission.

Do not move behavior across these seams merely to make an individual change convenient.

## Canonical invariants

* `registry/` is the reviewed canonical source tree.
* `dist/` is derived output. Contributors and agents must not manually create, edit, or commit it.
* `gh-pages` contains generated artifacts only, is updated only by trusted CI, and is the persistent cache for immutable release artifacts.
* Root `source.commit` is the exact processed `main` commit, never the `gh-pages` commit.
* `registry/modules/<owner>/<module>/manifest.json` and `versions/v<semver>.json` are the only production module source locations. Do not add fallback discovery for older layouts.
* `registry/plugins/` is required but reserved. Do not add plugin records until a real plugin registration contract exists.
* Registry identity is the exact lowercase `<owner>/<module>` coordinate. Reject mixed-case or mismatched input; never silently lowercase persisted identities.
* The Ferret runtime namespace is independent from Registry identity and remains case-sensitive.
* Every version record names a real public tag and pins the exact commit to which that tag resolves.
* Never create or accept Registry records for unpublished tags, placeholder commits, or rewritten historical tags.
* The installable Go package path comes from the pinned module's adjacent `go.mod`, not from the repository URL.
* Descriptive metadata and documentation location come from the pinned `ferret.yaml` at the declared source directory.
* Every release requires an exact pinned `README.md`; Barn publishes its bytes as version-local `docs.md` and a sanitized rendering as `docs.html`.
* `api.json` is derived from statically analyzable function registration in the pinned Go source. Do not copy the manifest's exports list into the generated API reference.
* Barn must fail closed when it cannot safely or unambiguously inspect a release. It must not emit partial artifacts.
* Generated artifacts are deterministic functions of validated canonical records and their exact pinned source content, except that publication time is read from the stored record.
* Incremental generation preserves unchanged version directories byte-for-byte and always rebuilds cheap global projections.
* `publishedAt` is assigned once after a new version reaches `main`. It is stored in the canonical version record and never regenerated from the clock or Git history.
* Existing published version source, identity, commit, tag, and `publishedAt` values are immutable.
* Contributor-supplied `publishedAt` values on new versions are invalid.
* Distribution links are part of the artifact contract. Consumers discover and follow them; they must not reconstruct internal `dist/` paths.

## Package and source map

Begin with the package or source area that owns the requested behavior. Do not infer ownership from a convenient call site when the responsibility is already defined below.

### CLI entry point

* `cmd/barn`
    * Owns the maintainer and CI command-line entry point.
    * Commands are `validate`, `generate`, `verify`, `verify-tree`, `check-immutable`, and `stamp`.
    * `generate` supports `full`, `auto`, and `incremental` modes plus explicit source, previous-tree, and output paths.
    * `--root` always means the Barn repository root containing `registry/`; default local output is `dist/` beneath that root.
    * Keep argument parsing and process-facing error reporting here. Registry behavior belongs in `internal/barn`.

### Canonical Registry loading and resolved model

* `internal/barn/load.go` and `internal/barn/types.go`
    * Own canonical source-tree discovery, strict directory expectations, source-record parsing, identity/path consistency, duplicate detection, and the resolved in-memory Registry model.
    * Production discovery is rooted only at `registry/modules` and the reserved `registry/plugins` directory.
    * Do not weaken unknown-file, unexpected-entry, non-directory, or identity validation to accommodate obsolete layouts.
* `internal/barn/run.go`
    * Coordinates loading and release resolution for validation.
    * Keep orchestration thin; validation details belong to the component that owns them.

### Git, source materialization, and package metadata

* `internal/barn/git.go`
    * Owns public repository checks, exact tag and commit resolution, clean Git execution, and coordination of pinned release inspection.
    * Remote release validation accepts anonymous HTTPS Git repositories only.
    * Redirects, credentials, unsafe protocols, and non-public destinations must remain rejected.
* `internal/barn/source.go`
    * Owns exact commit materialization and source-tree safety.
    * Reject symlinks, non-regular entries, unsafe paths, and local replacements that escape the materialized source tree.
    * Source inspection must not check out into or mutate the caller's repository.
* `internal/barn/package.go`
    * Owns parsing and validation of the pinned `go.mod` module directive against the release version.
    * Do not derive package paths from hosting URLs or Registry coordinates.

### Documentation projection

* `internal/barn/documentation.go`
    * Owns Markdown rendering, link rewriting against the explicit documentation URL, HTML sanitization, and stable browser-ready output.
    * Preserve the exact pinned README bytes for `docs.md`.
    * Do not fetch the documentation URL or search for alternative documentation filenames.
    * Sanitization and URL handling are security-sensitive; add focused negative tests for any change.

### API reference analysis

* `internal/barn/apiref`
    * Owns static analysis of Ferret function registrations and construction of the version-scoped API reference.
    * It analyzes source; it must never execute module code.
    * Supported registration forms are deliberately explicit. Dynamic or ambiguous forms must produce an analysis error rather than incomplete output.
    * Configuration branches are analyzed as a union so optional and legacy registrations remain visible.
    * Constants, types, methods, properties, host values, and query dialects are outside the current generated API contract.
    * Source position and error-kind quality are part of the validation experience; preserve both when changing analysis.

### Distribution generation and verification

* `internal/barn/distribution.go`
    * Owns projection from the resolved Registry into the complete static artifact hierarchy.
    * Owns module, version, category, documentation, and API-reference documents and their relative links.
    * Output ordering, JSON bytes, display-name derivation, version selection, and paths must remain deterministic.
    * `WriteDistribution` replaces `dist/` atomically.
    * `VerifyDistribution` rejects missing, stale, unexpected, symlink, and non-regular output.
    * Change Specs artifact types or validation in Specs, not through Barn-local wire-schema copies.
* `internal/barn/generation.go` and `internal/barn/distribution_cache.go`
    * Own source-commit resolution, full/auto/incremental selection, validated published-state loading, changed-release enrichment, and candidate validation.
    * Treat any non-Registry source change as a full-rebuild trigger and reject malformed, divergent, unsafe, or source-inconsistent cache state.
    * Reuse only complete validated immutable release bundles; never reuse partial or ambiguous artifacts.

### Immutability and publication timestamps

* `internal/barn/immutable.go`
    * Owns comparison of the current canonical source state against a Git base.
    * Existing published versions must not be deleted, relocated, or altered except for the one allowed absent-to-present publication-stamp transition.
    * Keep comparisons strict and based on canonical source records, not generated output.
* `internal/barn/stamp.go`
    * Owns assigning a whole-second UTC RFC3339 timestamp to records that lack `publishedAt` and checking publication completeness.
    * Stamping is idempotent for already stamped records.
    * Never rewrite an existing timestamp or derive it again during generation.

### Public static Registry client

* `pkg/registry`
    * Owns the reusable HTTP client for the generated static distribution.
    * Starts at the root artifact and follows validated links rather than constructing distribution paths.
    * Owns transport errors, HTTP status errors, same-origin navigation, and cross-document consistency checks.
    * Delegates strict artifact parsing and same-document schema validation to Specs.
    * Public types, options, error classification, URL resolution, and search behavior are API-sensitive.

### Public release preparation API

* `pkg/publish`
    * Owns `Prepare`, preparation stages, new-module/new-version classification, and deterministic Barn-relative `File` records.
    * Reuses the same pinned release inspection path and canonical schemas as Barn validation.
    * Must not write either repository, upload artifacts, resolve user credentials, call a Git hosting API, or create branches and pull requests.
    * Public result ordering, record bytes, stages, error wrapping, and idempotent classification are API-sensitive.

### Reviewed Registry records

* `registry/modules`
    * Contains canonical human-reviewed module manifests and version records.
    * A registration change should contain only the intended source records.
    * New versions must omit `publishedAt`; the post-merge workflow owns the initial stamp.
    * Do not modify old records as collateral cleanup.
* `registry/plugins`
    * Reserved and intentionally empty until plugin registration is implemented.
* `dist`
    * Ignored, generated, disposable output.
    * It may be generated locally for verification, but must not be hand-edited or committed.

## Where to start by task

* Change Registry source layout or record discovery:
    * inspect `internal/barn/load.go` and its tests
    * inspect Specs Registry source contracts
    * verify immutability and distribution assumptions before changing paths
* Change remote tag, commit, or repository validation:
    * inspect `internal/barn/git.go`
    * inspect source materialization and public-address protections
    * add positive and negative Git fixtures without weakening production security
* Change source-tree safety or local replacement handling:
    * inspect `internal/barn/source.go`
    * cover unsafe paths, symlinks, non-regular entries, and escape attempts
* Change package-path derivation:
    * inspect `internal/barn/package.go`
    * validate the pinned `go.mod` directive and version relationship
    * do not add repository-URL inference
* Change generated documentation:
    * inspect `internal/barn/documentation.go`
    * inspect distribution placement in `internal/barn/distribution.go`
    * test raw Markdown preservation, rewritten URLs, anchors, and sanitization
* Change API-reference extraction:
    * inspect `internal/barn/apiref`
    * identify whether the requested registration form is statically unambiguous
    * test successful extraction and fail-closed unsupported forms
* Change generated artifact shape or paths:
    * begin with the owning Specs artifact contract
    * inspect `internal/barn/distribution.go`
    * update client navigation and cross-document validation when the public contract changes
* Change category behavior:
    * inspect selected-release metadata and distribution projection
    * preserve explicit flat categories and deterministic indexes
    * do not infer categories from namespaces, names, or directories
* Change publication stamping:
    * inspect `internal/barn/stamp.go`, `internal/barn/immutable.go`, and both workflows
    * preserve the single absent-to-present transition and idempotency
* Change immutable-history enforcement:
    * inspect `internal/barn/immutable.go`
    * test the exact allowed transition plus deletion, relocation, and mutation failures
* Change public Registry client behavior:
    * inspect `pkg/registry`
    * preserve link discovery, same-origin rules, typed errors, cancellation, and deterministic search behavior
* Change release preparation:
    * inspect `pkg/publish`
    * reuse internal inspection and Specs validation rather than duplicating them
    * preserve the no-write and no-hosting-API boundary
* Change a Registry registration record:
    * verify that the public tag already exists and resolves to the pinned commit
    * validate the exact pinned manifest, package, README, and source
    * do not add `publishedAt`, touch `dist/`, or alter unrelated records

## Specs and cross-repository ownership

Barn consumes contracts from `github.com/MontFerret/specs`; it does not own portable Registry schemas.

* Change strict JSON parsing, field definitions, schema-level validation, JSON-pointer diagnostics, or artifact same-document rules in Specs first.
* Keep schemas closed. Do not accept unknown fields in Barn as a compatibility shortcut.
* Barn owns behaviors that require transport, Git, filesystem inspection, multiple source records, multiple artifact documents, or publication history.
* `pkg/registry` may wrap Specs parse or validation failures with endpoint and transport context, but it must not replace the underlying contract.
* When upgrading Specs, update module metadata deliberately and run the affected public-package and Registry validation suites.

## Public API rules

Treat `pkg/registry` and `pkg/publish` as external APIs.

* Do not export new symbols unless the task requires a new external contract.
* Prefer unexported helpers in the owning package before expanding the public surface.
* Preserve error identity and `errors.Is`/`errors.As` behavior when wrapping failures.
* Add doc comments for exported contracts and examples when a new usage pattern is introduced.
* Keep context cancellation and caller-supplied HTTP clients effective throughout client requests.
* Keep `publish.Prepare` deterministic and free of repository writes and hosting-provider concerns.
* Treat public field names, enum values, error stages, ordering, and JSON-facing examples as compatibility-sensitive.

## Registry source and distribution rules

* Never edit `dist/` to fix generation. Fix the canonical record, pinned source, Specs contract, or generator that owns the incorrect output.
* Generation must not consult the current clock for artifact content.
* The stored `publishedAt` value is the only publication timestamp source used by generation.
* Sort filesystem discovery and map-derived collections before emitting observable output.
* Validate generated documents with the owning Specs validator before writing them.
* Preserve atomic replacement so failed generation cannot leave a partially updated public tree.
* Verification must compare the complete expected tree, including unexpected files and unsafe file types.
* Pull-request-generated output is a validation artifact and is discarded. Only a fully stamped `main` state is committed to `gh-pages`.

## Publication and immutability rules

The publication lifecycle is intentionally asymmetric:

1. A contributor submits a new manifest/version record without `publishedAt`.
2. Pull-request CI uses a fixed preview timestamp only to prove generation.
3. After merge, the stamping workflow assigns the real timestamp to missing records and commits that transition.
4. CI validates the transition and commits a complete candidate to `gh-pages` only when every version is stamped.
5. The source identity, tag, commit, and timestamp are immutable thereafter.

Agents must not:

* pre-stamp contributor records;
* rewrite a published timestamp;
* backfill history from Git as part of routine generation;
* delete or rename a published version record;
* change a published tag or commit;
* weaken immutable checks to permit a desired record edit; or
* rewrite historical release tags in an upstream module repository.

If a published release is defective, create and publish a new version according to the release process rather than mutating history.

## Remote inspection and security rules

Remote validation handles untrusted repository locations and untrusted source trees.

* Permit only anonymous public HTTPS Git access in production inspection.
* Keep credentials, interactive prompts, configured credential helpers, redirects, and unsafe Git protocols disabled.
* Validate resolved IP addresses and reject non-public destinations to preserve SSRF protections.
* Inspect only the exact verified commit to which the declared public tag resolves.
* Reject tag/commit mismatches rather than selecting a nearby revision.
* Materialize into a task-scoped temporary directory, never the caller's working tree.
* Reject unsafe paths, symlinks, special files, and source-tree escapes.
* Keep Go analysis isolated with `GOWORK=off`, `GOENV=off`, read-only module behavior, disabled build VCS stamping, disabled CGO, and the fixed `linux/amd64` target.
* Do not execute module initialization, tests, generators, binaries, or arbitrary source code during inspection.
* Keep VCS dependency fallback disabled for API analysis; dependency resolution is limited to the configured public module proxy and checksum database.
* Preserve timeouts and context cancellation across network, Git, and analysis work.

Security-sensitive changes require explicit negative tests. Do not relax a guard merely because a fixture or local repository is easier to use without it; test-only local behavior must stay clearly separated from production policy.

## API-reference analysis rules

The analyzer recognizes a bounded set of static registration patterns described in `README.md` and enforced by tests.

* Extend the analyzer only for registration shapes that resolve to one deterministic namespace, name, function target, and signature.
* Analyze configuration alternatives as a union when all branches are statically understandable.
* Preserve stable fallback parameter names for missing or blank Go parameter names.
* Include Go-doc text only when the registered value resolves unambiguously to a named declaration.
* Do not infer or fabricate registrations from reflection, dynamic strings, loops, maps, interface dispatch, ambiguous factories, or external opaque helpers.
* Do not emit a partial API reference after an unsupported registration mutation is found.
* Modules with hooks or host values but no registered Ferret functions should still emit a valid empty namespace list.
* Keep analyzer diagnostics tied to stable error kinds and normalized source positions.

## Engineering principles

* Preserve correctness first.
* Preserve existing observable behavior unless the task explicitly requires changing it.
* Identify behavioral ownership before changing implementation.
* Preserve architectural boundaries and lifecycle invariants.
* Prefer the smallest coherent change that fully solves the task.
* Prefer straightforward, idiomatic Go over clever implementations.
* Keep behavior, state ownership, dependencies, cancellation, cleanup, and resource lifetimes obvious.
* Avoid abstractions, indirection, and generalization without a concrete need.
* Do not optimize by intuition alone. Measure performance-sensitive work.
* Reuse existing patterns only after verifying that they have the same semantics, ownership, and lifecycle.
* Existing technical debt is not precedent.
* Leave already-correct code alone.
* Do not treat the first working implementation as final.
* A task is complete only after implementation, validation, self-review, necessary corrections, final validation, and complete diff inspection.

Barn's deterministic output, immutable publication history, fail-closed inspection, and security boundaries are correctness requirements, not optional quality improvements.

## Ownership and design

Before making a non-trivial change, identify:

1. the subsystem, package, or type that owns the requested behavior;
2. the observable contract being preserved or changed;
3. the Registry, publication, security, or lifecycle invariants involved;
4. resource ownership and cleanup, where applicable;
5. compatibility surfaces, where applicable;
6. whether the change is concurrency-sensitive; and
7. whether the change is performance-significant.

Begin with the code that owns the behavior. Use the package and source map in this guide rather than inferring ownership from a convenient call site.

Do not move behavior into a caller, adapter, command, transport, or presentation layer merely because that call site is convenient.

Keep Registry domain behavior separate from transport, serialization, presentation, Git hosting, and protocol translation.

Adapters should validate and translate boundary values, delegate behavior to the owning implementation, and translate results back. They should not become alternate owners of domain semantics.

Avoid duplicated semantics. When Specs or a Barn subsystem owns a rule, consumers should use that rule rather than independently reproducing it.

Keep reusable behavior at the narrowest ownership level that preserves clear responsibility and testability.

Do not expose implementation details across boundaries merely to avoid making a proper change in the owning layer.

## Abstraction discipline

Prefer concrete types and direct implementations until there is a real need for abstraction.

Introduce an interface when there is:

* an actual substitution boundary;
* more than one meaningful implementation;
* a focused consumer-side contract; or
* a concrete test seam that materially improves the design.

Interfaces are usually most useful at the point of consumption.

Do not introduce interfaces, wrappers, managers, factories, helpers, generic types, or abstraction layers merely:

* for aesthetic symmetry;
* to reduce a few repeated lines;
* because another codebase uses the pattern;
* because the pattern is fashionable;
* to prepare for hypothetical future requirements;
* solely to make mocking convenient; or
* to make files shorter.

Similar-looking code is not sufficient reason to share an implementation.

Extract shared behavior only when the duplicated code represents the same concept with the same semantics, ownership, and lifecycle.

Do not force Registry, transport, Git, source inspection, or artifact concepts into a generic abstraction merely because their implementations have structural similarities.

Prefer deletion and simplification over another abstraction layer. An abstraction must make Barn easier to reason about, not merely make the code look more architecturally sophisticated.

## Semantic types

Introduce a named type when it can own meaningful:

* semantics;
* invariants;
* behavior;
* validation;
* conversion;
* lifecycle; or
* API safety.

Do not introduce a wrapper merely to give a primitive another name.

Once an established semantic type exists, APIs should normally use that type rather than repeatedly bypassing it with its underlying primitive.

Do not leave a meaningful Registry or publication type disconnected from its intrinsic behavior while free functions continue operating on primitive representations.

Unless zero has a natural and safe domain meaning, reserve zero as the unspecified or invalid value for enum-like types and begin meaningful values after it. Keep sibling enum-like APIs consistent and document intentional meaningful-zero exceptions.

## Dependencies and construction

Required dependencies must be explicit.

Construct required services or dependencies once at a clear composition root and pass them into consumers.

A constructor must not interpret a nil required dependency as a request to construct a hidden default.

Tests should construct required dependency graphs explicitly rather than relying on production-only hidden initialization.

Optional callbacks, options, or dependencies may have defaults only when their optional nature and default behavior are intentional and clear.

Avoid service locators, hidden globals, implicit initialization, and invisible dependency construction when explicit construction is practical.

Keep option validation, trimming, normalization, and defaulting close to the option-owning type or constructor. Do not repeat normalization rules across unrelated layers.

## Nil semantics

Do not silently assign convenient semantics to nil.

Required dependencies should reject or make impossible nil values rather than converting nil into hidden defaults.

Require non-nil `context.Context` values at operation boundaries. Do not silently replace nil with `context.Background()`.

Do not normally make nil receivers valid domain objects or map nil receivers to lifecycle states such as closed.

Use nil semantics only when nil is genuinely part of the intentional contract.

## Resource ownership and lifecycle

Make resource ownership visible in APIs.

A type that owns a closable resource should normally expose the lifecycle operation itself rather than exposing a DTO-like field that callers must discover and close manually.

When the distinction matters, make clear whether resources are owned, borrowed, leased, or transferred.

Release partially acquired resources on every failure path.

Cleanup must remain correct on:

* successful completion;
* errors;
* cancellation;
* early returns;
* partial initialization; and
* repeated shutdown or close operations when idempotency is part of the contract.

Do not eagerly materialize, retain, copy, or promote expensive resources without a concrete need.

When values can escape their current execution or ownership scope, make the resulting ownership transition explicit.

Lifecycle transitions should have one authoritative representation. Derived flags, events, atomics, and channels must not become competing sources of truth.

Do not represent the same lifecycle independently through several synchronization mechanisms without a concrete reason.

## Context and cancellation

Accept `context.Context` at operation boundaries that can block, perform I/O, be canceled, perform potentially long-running work, or participate in a caller-owned lifecycle.

Check or propagate cancellation early enough to avoid committing state after cancellation.

Propagate caller contexts rather than replacing them with `context.Background()` without a concrete protocol or lifecycle reason.

Do not store contexts in long-lived structs. Store explicit lifecycle state and cancellation functions when ownership requires them.

Long-running work must not outlive its owning context without an explicit lifecycle reason.

Preserve cancellation through HTTP requests, Git operations, source materialization, Go analysis, Registry navigation, and release preparation.

## Concurrency

Every goroutine must have a clear owner, a termination condition, and a cleanup path.

Reason about goroutine termination under normal completion, errors, cancellation, partial startup, and repeated shutdown. Avoid goroutine leaks.

Identify which mutex protects each field or cohesive state group.

Keep lock scope narrow and make protected state obvious from the type layout or a focused invariant comment.

Prefer one cohesive lock-owned representation when fields participate in the same lifecycle transition.

Do not mix mutexes, atomics, channels, once-guards, and duplicate flags for the same state without a concrete ordering or performance reason.

Do not call unknown, external, blocking, or potentially re-entrant code while holding a lock unless the ordering requirement is explicit and tested.

Do not hold service locks across blocking I/O or potentially long-running operations unless a documented invariant requires it.

Return copies or immutable views when callers must not mutate synchronized internal state.

Preserve required ordering between state changes and externally visible events.

Scrutinize repeated hand-written lifecycle or synchronization state machines. Extract a shared mechanism only when the semantics and ownership genuinely match.

Never trade obvious domain ownership for a clever concurrency abstraction.

Concurrency comments should explain ownership, invariants, and non-obvious ordering rather than narrating individual statements.

For changes that add or materially alter shared mutable state or goroutine coordination, add deterministic lifecycle or concurrency tests and run the race detector on affected packages.

## Error handling

Use standard Go error mechanisms.

Preserve error identity with `%w`, `errors.Is`, and `errors.As` when callers need classification.

Add context at subsystem and process boundaries without repeating the entire call chain.

Keep sentinel errors for stable conditions callers need to classify. Use typed errors when they express a meaningful structured contract.

Do not compare error strings in production code when `errors.Is`, `errors.As`, a sentinel, or a typed error can express the contract.

Distinguish cancellation, invalid input, missing or stale state, dependency failure, transport failure, Registry or runtime failure, and internal invariant violations when callers need different behavior.

Do not collapse expected user or domain failures and internal invariant violations into the same conceptual error class.

Do not log and return the same error at every layer. The owning process, transport, or presentation boundary should decide how to report it.

Error messages should normally be concise lowercase sentence fragments unless proper names or externally defined text require otherwise.

## Go type and file structure rules

These rules apply to new or substantially reworked handwritten Go unless the task explicitly requires otherwise. Existing focused exceptions do not justify adding new structural violations, and agents must not split untouched files solely to make older code conform.

* Keep each file centered on one narrow responsibility.
* Prefer declaring a method-bearing struct as a standalone `type Name struct { ... }`.
* A substantial method-bearing struct should usually live in its own file, named after its primary type or responsibility whenever practical, for example:
    * `client.go` for `Client`
    * `walker.go` for `walker`
    * `distribution.go` for distribution generation and persistence
* Avoid defining multiple substantial method-bearing structs in the same Go file.
* Grouped `type ( ... )` declarations are appropriate for interfaces, passive data-only structs, enums, and small related helper or wire-projection types from one narrow concern.
* A grouped declaration may include one primary method-bearing struct alongside passive helper types from the same concern.
* Focused `errors.go` files may group small related error wrapper types and their `Error`, `Unwrap`, or `Is` methods.
* Do not use grouped declarations to hide multiple unrelated behavioral types.
* If a passive helper gains substantial behavior and would create a second behavioral responsibility in the file, extract it into its own file.
* Keep methods in the same file as their owning type when practical. Split methods only when an established package structure or a large, coherent concern makes the separation clearer.
* Do not place a new method-bearing struct into an existing file merely because it shares the package and the code compiles.
* Do not add wrapper layers, interfaces, or packages only to make a small change appear generalized.
* Reuse existing helpers for canonical paths, validation, error wrapping, sorting, Git execution, and test fixtures.

Allowed:

```go
type (
	moduleProjection struct {
		ID     string
		Latest string
	}

	categoryProjection struct {
		ID      string
		Modules []moduleProjection
	}
)
```

These are passive values from the same distribution concern.

Avoid:

```go
type (
	Client struct {
		// ...
	}

	releaseInspector struct {
		// ...
	}
)
```

These types own different behavior and should not be hidden in one grouped declaration.

## Function and method ownership rules

These rules apply to new or substantially reworked code unless the task explicitly requires another organization.

* A file centered on a primary method-bearing type should normally contain the type, its methods, and its constructors.
* Prefer a method when behavior belongs intrinsically to a semantic type, depends on its invariants, naturally queries or transforms it, manages its resources, or operates primarily on receiver-owned state.
* Prefer a package-level function when behavior constructs a value, combines unrelated values, performs genuinely package-wide work, converts between values with no natural receiver, or has no meaningful owning type.
* Do not turn every helper into a method merely for stylistic uniformity.
* Do not introduce a meaningful domain type while leaving its intrinsic behavior in free functions that accept primitive representations.
* Do not mix unrelated package-level helpers into a type-centered file.
* Focused files such as `errors.go` may contain methods for a small family of related error wrappers.
* Keep orchestration functions thin; move validation, transformation, transport, and persistence details to the component that owns them.
* Keep conversion helpers near the boundary or concern they serve.
* Do not refactor an existing file merely because it predates these rules. When a requested change materially expands an existing structural problem, improve the touched area without broad unrelated churn.

## Package, file, and abstraction organization

Do not use `helpers.go`, `utils.go`, `common.go`, or similarly generic files as long-term containers for unrelated functionality.

A helper-focused file is acceptable while its contents represent one cohesive concern. As a concern grows, organize files around responsibilities a reader can predict, such as lifecycle, conversion, snapshots, parameters, identifiers, protocol state, or validation.

Keep package boundaries domain-oriented.

Do not create a package solely to shorten files, remove a few repeated lines, manufacture an abstraction layer, or avoid keeping related private implementation together.

Prefer cohesive private implementation types and files over unnecessary package fragmentation.

Keep symbols unexported until another package has a real need for them.

Avoid both files, functions, types, or packages doing too much and tightly related behavior being fragmented across excessive helpers, files, interfaces, or packages.

Behavioral ownership should be predictable from code organization.

## Comment rules for functions and methods

* Do not add comments to every function or method by default.
* Exported functions and methods should have doc comments, especially in `pkg/registry` and `pkg/publish`.
* Unexported functions and methods should be commented only when they carry non-obvious behavior, invariants, security constraints, side effects, ownership rules, cleanup expectations, or lifecycle requirements.
* Comments must explain intent, contract, invariants, side effects, or lifecycle behavior.
* Prefer comments that explain why the code exists, what must remain true, or how it is meant to be used.
* For Git inspection, source materialization, distribution generation, immutability, and publication code, describe the safety or history invariant instead of narrating individual statements.
* Do not write comments that merely restate the function name or signature.
* Keep future plans out of code comments unless the comment describes a deliberate current boundary.
* Update or remove comments when implementation changes make them obsolete.
* Avoid comment wallpaper. Dense, meaningful comments are preferable to mechanical documentation of obvious code.

Preferred for a public contract:

```go
// Prepare validates a public tagged release and returns deterministic Barn
// source records. It does not modify either repository or call a hosting API.
func Prepare(ctx context.Context, request Request) (*Result, error)
```

Preferred for internal policy:

```go
// resolveLink rejects cross-origin artifact navigation so a Registry document
// cannot redirect the client to an unrelated content host.
func (client *Client) resolveLink(parent *url.URL, href string) (*url.URL, error)
```

Avoid:

```go
// Prepare prepares a release.
func Prepare(ctx context.Context, request Request) (*Result, error)
```

## Go control-flow spacing rules

These rules are mandatory for handwritten Go code.

Blank lines should separate logical units and make control-flow and termination boundaries visually obvious.

### Immediate producer + check

A declaration, assignment, function call, type assertion, lookup, parse operation, or similar statement may remain directly adjacent to a following `if` when the `if` immediately checks or consumes the value produced by that statement.

This includes error checks, boolean/result checks, type assertions, nil checks, bounds checks, and other immediate validation.

Preferred:

```go
res, err := doSome()
if err != nil {
	return err
}
```

Preferred:

```go
named, ok := typeOf.(*types.Named)
if !ok || named.Obj().Pkg() == nil || !w.localPackage(named.Obj().Pkg().Path()) {
	return w.source.errorAt(
		ErrorUnsupportedRegistration,
		expression.Pos(),
		"New selects a module root dynamically",
	)
}
```

Preferred:

```go
value := lookup(name)
if value == nil {
	return ErrNotFound
}
```

Preferred:

```go
count := len(items)
if count == 0 {
	return nil
}
```

The producer and its immediate check form one logical unit and should not be separated by a blank line.

### Separation from preceding logic

If an immediate producer + check unit follows another statement or logical unit, separate it from the preceding code with a blank line.

Preferred:

```go
prepareState()

named, ok := typeOf.(*types.Named)
if !ok {
	return ErrUnsupported
}
```

Avoid:

```go
prepareState()
named, ok := typeOf.(*types.Named)
if !ok {
	return ErrUnsupported
}
```

No leading blank line is required when the producer begins the enclosing block:

```go
func inspect(typeOf types.Type) error {
	named, ok := typeOf.(*types.Named)
	if !ok {
		return ErrUnsupported
	}

	return inspectNamed(named)
}
```

### Consecutive control-flow blocks

Separate independent `if` statements with a blank line.

Avoid:

```go
if foo != nil {
	useFoo(foo)
}
if bar != nil {
	useBar(bar)
}
```

Prefer:

```go
if foo != nil {
	useFoo(foo)
}

if bar != nil {
	useBar(bar)
}
```

This applies even when both conditions are short. Independent control-flow decisions should remain visually distinct.

### Statements after control flow

Add a blank line after a completed `if` block before continuing with a separate statement or logical unit.

Avoid:

```go
if foo == bar {
	doFoo()
}
doSomething()
```

Prefer:

```go
if foo == bar {
	doFoo()
}

doSomething()
```

### Return and break separation

`return` and `break` are termination or control-transfer statements and should be visually separated from preceding statements.

A `return` or `break` must begin a new logical group: when another statement precedes it in the same block, place a blank line immediately before it.

This rule applies inside nested control-flow blocks as well as at the function-body level.

Avoid:

```go
if is(offending.Prev().Prev(), "NOT") {
	return "NOT EXISTS", offending.Prev()
}
return "EXISTS", offending.Prev()
```

Prefer:

```go
if is(offending.Prev().Prev(), "NOT") {
	return "NOT EXISTS", offending.Prev()
}

return "EXISTS", offending.Prev()
```

Avoid:

```go
if is(curr, "WAITFOR") {
	foundWaitFor = true
	break
}
```

Prefer:

```go
if is(curr, "WAITFOR") {
	foundWaitFor = true

	break
}
```

The same rule applies when ordinary computation precedes a return:

Avoid:

```go
result := buildResult()
return result
```

Prefer:

```go
result := buildResult()

return result
```

Likewise for `break`:

Avoid:

```go
found = true
break
```

Prefer:

```go
found = true

break
```

No blank line is required before a `return` when it is already the first statement in its block:

```go
if err != nil {
	return err
}
```

No artificial leading blank line should be introduced:

```go
func value() int {
	return 42
}
```

The intent is not to surround every `return` or `break` with whitespace. The rule specifically requires separation from a preceding statement in the same block.

## Local type declarations

Local types declared inside functions are allowed, but should be used deliberately.

Prefer a local type when all of the following are true:

* it is small;
* it is passive and method-free;
* it is used only within that function;
* it exists solely to support the local algorithm; and
* keeping it local makes the function easier to understand rather than harder to scan.

Prefer a package-level unexported type when one or more of the following are true:

* the type represents a meaningful Registry, transport, analysis, or projection concept;
* it is used across a substantial portion of a long or complex function;
* moving it out of the control flow improves readability;
* it may reasonably gain methods or behavior;
* it is likely to be reused by nearby helpers; or
* its name helps explain the algorithm or responsibility at package scope.

Do not promote a tiny throwaway struct to package scope merely for consistency. Do not keep a meaningful concept local merely to avoid adding a package-level type.

Appropriate local type:

```go
func collectPaths(files map[string][]byte) []string {
	type entry struct {
		path string
		size int
	}

	entries := make([]entry, 0, len(files))
	for path, data := range files {
		entries = append(entries, entry{path: path, size: len(data)})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].path < entries[j].path
	})

	paths := make([]string, len(entries))
	for i, item := range entries {
		paths[i] = item.path
	}

	return paths
}
```

Prefer a package-level type when the value has domain meaning across generation steps:

```go
type moduleProjection struct {
	ID       string
	Latest   string
	Versions []versionProjection
}
```

The choice should be based on readability, conceptual ownership, and expected evolution rather than a blanket preference for local or package-level declarations.

## Response and code style

When assisting with this repository, avoid large unstructured blocks of prose or code.

* Use short sections with clear headings when structure is useful.
* Use bullets for decisions, trade-offs, validation results, and follow-up work.
* Use code blocks only for actual code, commands, or configuration.
* Prefer focused snippets or diffs over full-file dumps.
* Explain the owning concern and relevant invariant before describing a non-obvious implementation.
* Keep comments in code useful and minimal.
* Avoid repeating the same context in multiple sections.
* Distinguish successful source validation from network, DNS, Git, proxy, cache, or sandbox failures.
* Report only tests, generation, review, and validation that actually ran.

## API and compatibility discipline

Treat observable behavior as intentional until the task establishes otherwise.

Do not change public, external, wire-visible, CLI-visible, persistence-visible, generated-artifact, or integration-visible behavior as collateral cleanup.

Treat `pkg/registry`, `pkg/publish`, generated Registry documents, canonical source records, command output, error classification, ordering, paths, and links as compatibility-sensitive surfaces.

Do not export new symbols merely to share implementation internally. Prefer unexported helpers inside the owning package before expanding an API surface.

When a new exported symbol is genuinely necessary, document the external contract clearly.

For compatibility-sensitive changes:

* make the behavior change explicit;
* preserve previous behavior unless incompatibility is required;
* add focused tests at the observable boundary;
* document intentional incompatibility; and
* avoid unrelated contract changes.

Do not infer desired behavior from historical discussions, abandoned designs, stale comments, obsolete Registry layouts, or future-looking architecture when current implementation and tests establish a different contract.

## Testing standards

Add or update tests for every behavior change.

Put tests beside the package or layer that owns the behavior whenever practical.

Test observable contracts rather than mirroring implementation details.

Prefer focused table-driven tests when several inputs exercise the same contract.

Use `t.Helper()` in reusable test helpers.

Use `t.Cleanup()` for restoring globals, closing resources, canceling contexts, or stopping goroutines.

Avoid sleeps as synchronization. Use channels, contexts, deadlines, barriers, or observable state.

Keep timeouts bounded and generous enough for CI while still detecting leaks and deadlocks.

Verify both success and failure paths.

Test relevant positive cases, negative cases, boundary conditions, invalid inputs, cancellation, cleanup, repeated operations, idempotency, stale state, error identity, and concurrency behavior.

For bug fixes, add a regression test that fails without the fix whenever practical.

When behavior crosses meaningful package or integration boundaries, include integration-level coverage rather than relying exclusively on direct method tests.

Do not add redundant tests that increase maintenance cost without protecting meaningful behavior.

Avoid brittle tests unnecessarily coupled to implementation details.

Assertions must verify meaningful behavior strongly enough that plausible regressions fail.

A passing test suite is evidence of correctness. It is not evidence that the design is good.

## Testing map

Place tests next to the owning behavior.

* Source layout, record loading, identity, and reserved plugin behavior: `internal/barn/load_test.go`
* Git resolution, public-address policy, and pinned release inspection: `internal/barn/git_test.go`
* Exact source materialization, unsafe entries, and local replacements: `internal/barn/source_test.go`
* Documentation rendering, rewriting, anchors, and sanitization: `internal/barn/documentation_test.go`
* API-reference registration analysis and diagnostics: `internal/barn/apiref/analyzer_test.go`
* Distribution hierarchy, determinism, categories, writing, and verification: `internal/barn/distribution_test.go`
* Published-record history rules: `internal/barn/immutable_test.go`
* Timestamp assignment and completeness: `internal/barn/stamp_test.go`
* Public Registry client transport, navigation, parsing, and consistency: `pkg/registry/client_test.go`
* Release preparation, classification, records, stages, and no-write behavior: `pkg/publish/prepare_test.go`
* Public usage examples: package-local `example_test.go` files

Tests should emphasize observable contracts rather than mirroring implementation details.

* Bug fixes should include a regression test that fails without the fix whenever practical.
* Validation changes should include accepted and rejected cases.
* Security changes should cover bypass attempts and fail-closed behavior.
* Distribution changes should verify exact paths, links, ordering, and stale/unexpected output handling.
* Public API changes should verify typed error behavior and cancellation where relevant.
* Publication changes should cover the one allowed stamp transition and forbidden historical mutations.
* Use temporary repositories, servers, directories, and deterministic clocks/timestamps in tests. Do not depend on live hosting services for unit coverage.

## Performance

Do not optimize by intuition.

A change is performance-significant when it could reasonably affect:

* execution throughput;
* latency on common or hot paths;
* allocation patterns;
* memory usage or retention;
* repeated parsing, analysis, Git inspection, conversion, or serialization;
* incremental-generation caching or artifact reuse;
* synchronization or lock contention;
* resource cleanup;
* materialization cost;
* startup or shutdown performance; or
* long-running process memory behavior.

Documentation-only, test-only, pure rename, formatting-only, and narrow non-hot-path refactoring changes are normally not performance-significant.

When uncertain whether a change affects a hot path, treat it as performance-significant and measure it.

For performance-significant changes:

1. Identify an existing focused benchmark or add one.
2. Run it before implementation and retain the baseline.
3. Implement the change.
4. Run the same benchmark afterward under comparable conditions.
5. Compare relevant metrics such as `ns/op`, `B/op`, and `allocs/op`.
6. Investigate meaningful regressions before considering the task complete.

Inspect performance-sensitive implementations for accidental allocations, unnecessary copying, repeated conversions or computation, unnecessary materialization, avoidable synchronization, increased lock contention, blocking work added to hot paths, unnecessary work added to disabled or unchanged-release paths, and resources retained longer than necessary.

Do not trade clear correctness or maintainability for speculative micro-optimization.

If no relevant benchmark exists for an affected hot path, add one when practical.

If benchmark tooling or the environment is unavailable, state that explicitly rather than claiming performance validation.

## Change discipline

Keep the diff focused on the requested task.

Do not perform opportunistic refactoring, dependency upgrades, formatting churn, API redesign, package reshuffling, abstraction creation, generated-file changes, documentation rewrites, Registry-record cleanup, or implementation of future features unless required by the requested change.

Do not modify unrelated code merely to make it conform stylistically.

Do not use an implementation task as an excuse to clean up the surrounding repository.

A cleanup discovered while working may be included when it is small, local, low-risk, clearly understood, directly related to the affected area, and beneficial to correctness, lifecycle safety, ownership, or maintainability of the requested change.

If a discovered issue requires broader architectural work, preserve current behavior and report it for a separate task.

Preserve unrelated dirty, modified, and untracked files. Do not overwrite, revert, or reformat unrelated user changes.

Do not update dependencies unless the task requires a dependency change.

Do not change compatibility-sensitive contracts as collateral cleanup.

Generated files must be changed through their source inputs or generator when the repository provides such a workflow. Do not manually edit generated output, and inspect generated diffs when generation is required.

## Required workflow for non-trivial changes

For every non-trivial coding task:

1. **Identify ownership.** Determine the Barn subsystem, package, type, source area, or external Specs contract that owns the requested behavior.
2. **Identify the contract.** Determine the observable behavior, invariants, compatibility requirements, lifecycle, resource ownership, error semantics, determinism, immutability, and security boundaries being preserved or changed.
3. **Understand the current implementation.** Read current source and tests before relying on architecture prose, historical discussion, old branches, or assumptions.
4. **Choose the smallest coherent design.** Prefer a local, comprehensible implementation that fits existing ownership boundaries.
5. **Evaluate risk.** Determine whether the change is security-sensitive, concurrency-sensitive, lifecycle-sensitive, compatibility-sensitive, generation-sensitive, or performance-significant.
6. **Establish a performance baseline when necessary.** Run relevant benchmarks before changing performance-sensitive code.
7. **Add or update correctness tests.** Define the observable behavior the implementation must satisfy, including focused negative tests for security-sensitive changes.
8. **Implement the change.** Keep the implementation focused on the requested behavior.
9. **Run focused validation.** Run the narrowest tests and checks that directly exercise the changed behavior first.
10. **Broaden validation according to risk.** Run package, integration, race, lint, build, generation, Registry, or repository-level validation as appropriate.
11. **Perform the mandatory final self-review.** Review the implementation itself rather than merely confirming that automated checks pass.
12. **Correct review findings.** Fix problems introduced by the task and appropriate directly adjacent issues according to the scope rules below.
13. **Re-run affected validation.** Repeat any validation invalidated by review-driven changes.
14. **Re-run affected benchmarks.** If review-driven corrections affect benchmarked code, repeat the relevant benchmark comparison.
15. **Inspect the complete final diff.** Review the change as one coherent unit.
16. **Report accurately.** State what changed, what was tested, what was measured, what was reviewed, and what remains unresolved.

Do not consider a task complete merely because the implementation compiles and its tests pass.

## Mandatory final self-review

Every non-trivial coding task must end with a deliberate design and implementation review before it is considered finished.

Review the final implementation as though reviewing another engineer's pull request.

The review must evaluate the code itself, not merely confirm that compilation, tests, lint, static analysis, generation, or benchmarks succeeded.

Review all changed code and directly adjacent code necessary to understand the change. Inspect the complete diff as a coherent change.

The purpose of self-review is to catch correctness, design, quality, organization, performance, security, determinism, and maintainability problems introduced or exposed by the task. It must not become justification for unrelated refactoring or redesign.

### Correctness and completeness

Verify:

* every requested behavior is implemented;
* explicit non-goals remain untouched;
* existing behavior is preserved unless intentionally changed;
* implementation assumptions are valid;
* boundary conditions and failure paths are handled;
* partial operations do not leave invalid state or partial artifacts;
* errors preserve required identity and context;
* cancellation works where applicable;
* resources are cleaned up on every relevant path;
* cleanup and shutdown are idempotent where required;
* lifecycle transitions remain valid;
* empty, duplicate, invalid, and stale state is handled correctly;
* ordering requirements remain correct;
* concurrent behavior remains correct;
* goroutines terminate;
* locks are not held across inappropriate work;
* generated output remains complete and deterministic;
* publication and immutable-history transitions remain valid; and
* public or externally observable semantics match the intended contract.

Look actively for missing cases and regressions rather than reviewing only the successful path.

For bug fixes, verify that a regression test fails without the fix whenever practical.

Ensure tests would detect plausible regressions rather than merely repeat implementation structure.

### Security and resource handling

Verify:

* untrusted URLs, Git repositories, paths, source entries, Markdown, and Go source remain contained;
* credentials, redirects, local-network access, unsafe protocols, and source execution have not been enabled accidentally;
* exact tag and commit pinning remains enforced;
* temporary resources, HTTP bodies, subprocesses, files, and materialized sources are cleaned up on success, failure, and cancellation;
* partially acquired resources are released;
* atomic write and replacement behavior remains intact where required;
* failed generation cannot leave partial artifacts; and
* security-sensitive failures remain fail-closed.

### Code clarity and cleanliness

Review the implementation for:

* unnecessary complexity;
* duplicated behavior or semantics;
* excessive nesting;
* awkward control flow;
* misleading naming;
* overly large functions;
* hidden state transitions or ownership;
* unnecessary mutation or indirection;
* difficult-to-follow execution paths;
* dead branches;
* temporary implementation artifacts;
* debugging output;
* obsolete helpers; and
* abandoned approaches left in comments or code.

The primary execution path should remain easy to follow.

Prefer straightforward code whose behavior can be understood locally.

Simplify code when the simpler implementation is clearly equivalent and easier to reason about. Do not perform stylistic rewrites merely because another form is also valid.

### Go design and API quality

Check:

* API consistency and naming;
* semantic-type grounding;
* method-versus-function ownership;
* constructor behavior;
* dependency construction;
* nil semantics;
* option ownership and normalization;
* enum zero values;
* resource ownership and lifecycle visibility;
* context propagation;
* error wrapping and classification;
* synchronization and lock scope;
* goroutine ownership; and
* cleanup behavior.

Look specifically for:

* meaningful types bypassed by primitive APIs;
* free functions containing behavior naturally owned by a type;
* methods whose behavior does not naturally belong to their receiver;
* required dependencies hidden behind nil defaults;
* ambiguous resource ownership;
* repeated option normalization;
* competing lifecycle representations; and
* generic helpers containing unrelated behavior.

Do not introduce a pattern merely because it is common or fashionable elsewhere. It must improve Barn specifically.

### Abstraction quality

Review every new abstraction critically.

Ask:

* Is this abstraction required by the current problem?
* Does it represent a real concept?
* Are its implementations genuinely substitutable?
* Does it clarify ownership?
* Does it reduce meaningful duplication?
* Does it simplify reasoning?
* Would direct concrete code be clearer?
* Is it preparing for hypothetical future requirements rather than solving a current need?

Remove abstractions that do not earn their complexity.

Do not generalize two concepts merely because their implementations look structurally similar when their semantics, ownership, or lifecycle differ.

### Architecture and APIs

Verify:

* behavior remains in the correct subsystem, package, type, and layer;
* portable schemas and same-document validation remain in Specs;
* Git, filesystem, transport, cross-record, and cross-document responsibilities remain in Barn;
* CLI and caller-owned hosting concerns have not leaked into `pkg/publish`;
* domain behavior has not leaked into transport, adapter, command, or presentation layers;
* adapters translate and delegate rather than becoming alternate implementations;
* implementation details have not leaked unnecessarily across package boundaries;
* semantics are defined once rather than duplicated by consumers;
* no artifact-path reconstruction has replaced link navigation;
* new exported APIs are genuinely necessary, documented, and compatibility-conscious;
* package boundaries remain meaningful;
* abstractions exist at the correct level; and
* the implementation has not accidentally incorporated speculative future architecture.

Consider whether the design will remain understandable as the feature evolves, without attempting to design hypothetical future features now.

### Code organization and split

Verify that files, types, methods, functions, and packages have coherent responsibilities.

Check compliance with the type and file structure rules and the function and method ownership rules.

Look for:

* files doing too much;
* types owning unrelated behavior;
* functions performing several unrelated operations;
* package-level helpers mixed into type-centered files;
* multiple substantial behavioral types hidden in one file;
* generic utility dumping grounds;
* meaningful concepts hidden as local implementation details; and
* unrelated responsibilities grouped together.

Also look for excessive helper extraction, unnecessary file splitting, tiny interfaces without meaningful boundaries, package fragmentation, forwarding-only layers, and abstractions whose only effect is additional navigation.

Keep tightly related behavior cohesive. Helpers should exist at the narrowest appropriate ownership level.

A reader should be able to predict where behavior lives from its responsibility.

### Comments and documentation

Re-read comments directly affected by the change.

Verify that comments describe current behavior, contracts, invariants, ownership, and lifecycle accurately and do not describe abandoned approaches or speculate unnecessarily about future architecture.

Remove comments made obsolete by clearer code. Do not add comments merely to compensate for an unnecessarily confusing implementation.

When a change alters user-visible, integration-facing, generated-artifact, or public API behavior, evaluate whether documentation must be updated.

Documentation synchronization is part of a behavior change when existing documentation would otherwise become incorrect. Do not use documentation impact as justification for unrelated documentation cleanup.

### Tests

Review the tests themselves, not only their result.

Look for missing positive cases, negative cases, boundary cases, invalid inputs, cancellation paths, cleanup paths, repeated-operation cases, idempotency cases, stale-state cases, error-classification cases, concurrency cases, security bypass attempts, and integration coverage where behavior crosses boundaries.

Check for weak assertions, tests that merely mirror implementation details, brittle dependence on internal structure, redundant coverage, flaky timing, sleeps used for synchronization, leaked goroutines or resources, mutable global state, and unnecessarily narrow happy-path coverage.

Verify that tests assert meaningful observable behavior.

For errors whose identity is part of the contract, test classification rather than only message strings.

For concurrency behavior, prefer deterministic lifecycle tests.

For user-visible behavior spanning multiple layers, ensure package-local tests are supplemented by appropriate boundary or integration coverage.

### Performance

For performance-significant changes, review the final implementation for accidental allocations, repeated work, unnecessary copying, conversions or materialization, unnecessary synchronization, lock contention, blocking work, memory retention, hot-path overhead, and expensive work added to optional or unchanged-release paths.

Compare final benchmark results against the pre-change baseline and verify that benchmark setup remained comparable.

Investigate meaningful regressions. Do not rationalize a regression merely because correctness tests pass.

Do not trade clear correctness or maintainability for speculative micro-optimization.

## Self-review findings and remediation

When self-review finds a problem, classify it before changing code.

### Problems introduced by the task

Fix every meaningful deviation introduced by the task.

This includes correctness problems, regressions, lifecycle problems, resource leaks, concurrency problems, ownership or architecture violations, API problems, security or determinism failures, significant maintainability problems, significant test-coverage gaps, and meaningful performance regressions caused by the change.

Do not leave such problems unresolved merely because the initial implementation works or tests pass.

### Directly adjacent pre-existing problems

A pre-existing issue may be fixed when the correction is small, local, low-risk, clearly understood, directly within the affected area, and relevant to correctness, ownership, lifecycle, architecture, or maintainability of the requested change.

Do not copy a poor existing pattern merely because it already exists. Existing technical debt is not precedent.

### Broader pre-existing problems

If a discovered problem requires broad refactoring, package restructuring, API redesign, unrelated cleanup, dependency changes, speculative architecture, or substantial additional behavior, leave it unchanged and report it as separate follow-up work.

Do not allow self-review to expand the task without a concrete reason.

## What self-review must not become

Do not use self-review as justification for speculative refactoring, unrelated cleanup, rewriting correct code for stylistic consistency, unrelated API redesign, broad package reshuffling, dependency upgrades, abstractions without a concrete need, future features, or semantic changes outside the requested task.

Distinguish actual problems from optional preferences.

Existing code that is clear, correct, idiomatic, appropriately designed, and appropriately organized should be left alone.

## Final diff inspection

Immediately before finishing every non-trivial task, inspect the complete final diff as a whole rather than reviewing only individual files in isolation.

Verify that:

* every changed line belongs to the requested task or a necessary supporting change;
* unrelated user changes remain intact;
* no temporary code, debugging output, dead code, or abandoned implementation remains;
* no accidental behavior, API, compatibility, dependency, or Registry-record changes slipped in;
* no unrelated refactors or formatting churn slipped in;
* generated files changed only when their source inputs required regeneration;
* `dist/` was not hand-edited or included accidentally;
* tests describe intended behavior rather than implementation details;
* comments describe current contracts and invariants;
* package, file, and type responsibilities remain coherent;
* method and function ownership remains coherent;
* cancellation, concurrency, cleanup, and resource lifetimes remain correct;
* deterministic generation, fail-closed validation, and immutable history remain intact; and
* the result is the smallest coherent change that fully solves the task.

If final inspection causes another change, repeat every validation or benchmark whose result may have been invalidated.

The final diff, not an earlier intermediate implementation, is what must satisfy this guide.

## Validation discipline

Run the narrowest validation that proves the changed behavior first, then broaden according to scope and risk.

Typical progression:

1. focused tests for the affected package or behavior;
2. directly affected integration tests;
3. race detection for concurrency-sensitive changes;
4. static analysis or lint where relevant;
5. broader repository tests;
6. build or compilation validation;
7. generation and verification checks when generated artifacts are involved; and
8. live Registry validation when the change affects pinned-release inspection and network access is available.

Do not run unrelated expensive validation merely to create validation theater.

Conversely, do not stop at a narrow unit test when the change affects behavior across packages or external boundaries.

After review-driven changes, re-run every command whose result may have been invalidated.

Never claim a validation command succeeded unless it was actually run successfully.

If validation cannot be completed because of tooling, environment, permissions, external dependencies, or time constraints, report the limitation explicitly.

## Tooling prerequisites

* Go 1.25 or newer is required by the module.
* CI uses Go 1.26.x because the published module set must type-check under that toolchain.
* `make` is the preferred entry point for repository workflows.
* Git is required for pinned release inspection and immutable-history checks.
* Network-dependent validation requires DNS, anonymous HTTPS Git access, and access to the public Go proxy and checksum database.

## Command matrix

Run the narrowest command that proves the changed behavior, then broaden in proportion to risk.

```sh
go test ./internal/barn/...          # Internal Registry behavior and API analysis.
go test ./pkg/registry               # Public static Registry client.
go test ./pkg/publish                # Public release preparation.
make fmt                             # Rewrite Go files with gofmt-compatible formatting.
make fmt-check                       # Check Go formatting without rewriting files.
make vet                             # Run go vet.
make build                           # Build all Go packages.
make tidy                            # Update go.mod and go.sum.
make mod-check                       # Verify go.mod/go.sum need no tidy changes.
make test                            # Run all Go tests.
make test-race                       # Run all tests with the race detector.
make validate                        # Validate canonical records and all pinned releases.
make generate                        # Replace dist/ with the generated distribution.
make verify                          # Verify the complete current dist/ tree.
make generate-pages OUTPUT=<path> PREVIOUS=<path>  # Build a full or incremental Pages candidate.
make verify-pages OUTPUT=<path> SOURCE_COMMIT=<rev> # Validate a candidate without remote enrichment.
make check-immutable BASE=<git-ref>  # Compare published source history with a Git base.
make stamp                           # Assign timestamps to unstamped canonical versions.
make check-stamped                   # Require every canonical version to be stamped.
make check                           # fmt-check, vet, test, and live Registry validation.
make help                            # List the current Makefile targets.
```

Important command distinctions:

* `make check` is the broad local default, but it does not include `make build`, `make mod-check`, `make test-race`, distribution generation/verification, or base-relative immutability checks. CI runs those separately where applicable.
* `make validate`, `make generate`, `make verify`, and therefore `make check` inspect real pinned releases and may require network access and uncached Go dependencies.
* `make generate` intentionally writes ignored `dist/`; run it only when generation is relevant.
* `make verify` expects a complete generated `dist/` tree and fails on any missing, stale, unexpected, or unsafe entry.
* `make fmt` and `make tidy` rewrite tracked files. Run them only when the task calls for the corresponding Go or module-metadata changes, and inspect their diffs.
* `make stamp` mutates canonical source records. Run it only for an explicitly authorized publication/operator task, never as routine validation.
* `make check-immutable` requires an explicit, correct Git base. Resolve the intended base before running it.

## Network and managed-environment failures

Live Registry validation is not a hermetic unit test. It resolves public repositories and may load pinned module dependencies.

* A DNS failure such as inability to resolve a Git host is an environment failure, not evidence that a Registry record is invalid.
* A blocked public Go proxy or checksum database can prevent API analysis before product code is exercised.
* Managed environments may block default Go build or module caches. Use task-scoped writable cache directories when needed; do not weaken product security or tests to work around the sandbox.
* Report the exact failed command and the environmental cause separately from source/test failures.
* Do not claim live validation passed when it was skipped, blocked, or satisfied only from stale generated output.

## Editing rules

* Do not hand-edit or commit `dist/`.
* Do not add records under `registry/plugins/`.
* Do not add `publishedAt` to a contributor-submitted version.
* Do not rewrite existing Registry records, tags, or timestamps to make a new registration pass.
* Do not duplicate Specs schema types or parsing logic locally.
* Do not add client-side assumptions about physical distribution paths; follow artifact links.
* Do not introduce network access into tests that can use deterministic local fixtures.
* Use `gofmt` through the repository workflow for handwritten Go changes.
* Keep changes focused and preserve unrelated working-tree changes.

## Validation expectations by change

* Documentation-only change:
    * inspect rendered Markdown or the raw diff as appropriate
    * run `git diff --check`
* Internal package-local behavior:
    * run the owning package tests
    * run `make fmt-check` and `make vet` for Go changes
* Public `pkg/registry` or `pkg/publish` behavior:
    * run the package tests and examples
    * run broader `make test`, plus `make mod-check` when dependencies change
* Registry source record:
    * run live `make validate`
    * run the applicable base-relative immutability check
    * do not generate or commit `dist/`
* Distribution or artifact projection:
    * run distribution tests
    * run `make generate` and `make verify`
    * ensure only ignored `dist/` output was produced
* Publication stamping or immutable history:
    * run focused stamp and immutable tests
    * test against an explicit Git base or deterministic fixture
    * inspect workflow implications
* Git, source safety, documentation sanitization, or API analysis:
    * run focused positive and negative tests
    * run broader internal tests
    * run live validation when the behavior affects real pinned releases and network access is available
* Workflow or toolchain change:
    * compare the Makefile, both workflows, `go.mod`, and README command documentation
    * run the closest local equivalents of changed CI steps

After any review-driven code change, re-run the checks that exercise that code. Never report validation from before the final implementation state.

## Validation evidence and final reporting

When finishing a non-trivial task, report:

* the owning subsystem;
* the files changed and behavior changed;
* public contracts changed, if any;
* important behavior and invariants preserved or intentionally changed;
* tests added or updated;
* commands actually run and their results;
* race-detector validation when applicable;
* benchmarks added or updated when applicable;
* benchmark commands and before/after comparison when applicable;
* whether live Registry validation was run or blocked by the environment;
* whether generated `dist/` output was produced and whether it remains untracked/ignored;
* the final self-review and diff inspection performed;
* meaningful issues found and corrected during self-review;
* noteworthy pre-existing issues intentionally left outside scope;
* remaining concerns or limitations; and
* environmental or tooling failures that prevented validation.

Do not claim:

* tests passed unless they were run;
* lint or static analysis passed unless it was run;
* builds passed unless they were run;
* race detection passed unless it was run;
* benchmarks were completed unless they were run;
* generation was verified unless it was verified;
* live Registry validation passed when it was skipped, blocked, or satisfied only from stale output; or
* self-review was completed unless the final implementation and complete diff were actually inspected.

Accuracy of the completion report is part of engineering quality.

## Decision bias when uncertain

When uncertain:

* inspect current source and tests before relying on assumptions;
* preserve existing observable behavior;
* identify ownership before adding behavior;
* prefer the smaller local change;
* prefer concrete code over speculative abstraction;
* keep dependencies explicit;
* make ownership and lifecycle explicit;
* preserve error identity;
* propagate cancellation;
* add a focused test;
* treat concurrency and security changes cautiously;
* measure when performance might be affected;
* preserve deterministic output and immutable history;
* fix concrete review findings before speculative cleanup;
* avoid expanding the task unnecessarily; and
* leave already-correct code alone.

When choosing between a clever implementation and an obvious implementation that satisfies the same requirements, prefer the obvious implementation.

When choosing between an abstraction that might become useful and concrete code that clearly solves the current problem, prefer the concrete code.

When choosing between broad cleanup and a focused change, prefer the focused change.

When choosing between assuming behavior and verifying it in source or tests, verify it.

## Definition of done

A non-trivial coding task is complete only when:

* ownership and contracts were understood;
* the requested behavior is implemented;
* relevant existing behavior is preserved;
* deterministic, immutable, and security-sensitive invariants remain satisfied;
* tests cover the meaningful behavior;
* relevant validation has passed;
* concurrency validation has been performed when applicable;
* performance has been measured when applicable;
* the implementation has undergone mandatory self-review;
* review findings introduced by the task have been corrected;
* affected validation and benchmarks have been repeated after corrections;
* the complete final diff has been inspected;
* the final change is focused and coherent; and
* completion results and limitations are reported accurately.

Compiling is not completion. Passing tests is not completion.

## Secondary references

* `README.md` for the Registry model, artifact hierarchy, supported API-reference patterns, registration workflow, and operator guidance.
* `Makefile` for local commands and their exact composition.
* `go.mod` for the library module's current minimum Go version and dependencies.
* `.github/workflows/ci.yml` for complete pull-request, main, publication-transition, and deployment validation.
* `.github/workflows/stamp-publications.yml` for the post-merge timestamp assignment lifecycle.
* Current package tests for executable behavior and edge-case contracts.
