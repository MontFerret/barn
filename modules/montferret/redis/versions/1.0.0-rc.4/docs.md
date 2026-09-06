# DB::REDIS

The Redis module provides reusable Redis connection handles under `DB::REDIS`. Redis commands use Ferret's generic `QUERY` syntax: the query string contains the complete Redis command template and `WITH` supplies named values.

Templates compile directly into ordered arguments for go-redis. Bound values are never interpolated into one string and split again, so a value such as `"Tim Voronov"` remains one Redis argument.

## Opening and Closing Connections

Open a connection with a standard `redis://` or TLS-enabled `rediss://` URL:

```fql
LET redis = DB::REDIS::OPEN({
    url: "redis://localhost:6379/0"
})

RETURN DB::REDIS::CLOSE(redis)
```

URLs can include username/password authentication, a database number, and go-redis query options. RESP3 is the default; use `?protocol=2` or `?protocol=3` to select a protocol explicitly.

`unix://` is intentionally unsupported because user-controlled filesystem paths must go through Ferret's filesystem policy. Connections are opaque resources: explicit `CLOSE` is idempotent, and Ferret closes live resources when execution ownership ends.

## Query Templates and Bindings

Use `$name` for a case-sensitive named binding:

```fql
LET profile = QUERY ONE "HGETALL user:$id"
    IN redis
    USING redis
    WITH {
        id: 42
    }
```

A standalone placeholder preserves its Ferret scalar as a structured Redis argument. Embedded placeholders compose one argument:

```fql
LET value = QUERY ONE "GET tenant:$tenant:user:$id"
    IN redis
    USING redis
    WITH {
        tenant: "acme",
        id: 42
    }
```

This sends `GET` and `tenant:acme:user:42` as two arguments. String, Int, Float, Boolean, and Binary bindings are supported. `NONE`, objects, and arrays without spread syntax are rejected.

`WITH` may be omitted when the template has no placeholders:

```fql
LET pong = QUERY ONE "PING" IN redis USING redis_exec
```

Missing bindings fail before Redis execution. Extra bindings are allowed and ignored.

## Spreading Arrays

Use `$name...` when an array or other Ferret list should become multiple Redis arguments:

```fql
LET users = QUERY "MGET $keys..."
    IN redis
    USING redis
    WITH {
        keys: ["user:1", "user:2", "user:3"]
    }
```

An empty list contributes zero arguments. Multiple spread placeholders are expanded in their original positions. Spread syntax must occupy a complete argument; forms such as `prefix:$keys...` and `$keys...:suffix` are rejected.

Set members use the same expansion:

```fql
LET added = QUERY ONE "SADD roles:$id $roles..."
    IN redis
    USING redis_exec
    WITH {
        id: 42,
        roles: ["admin", "editor"]
    }
```

## Reads and Mutations

Use `redis` only for commands the connected server marks as read-only:

```fql
LET leaders = QUERY "ZRANGE scores $start $stop WITHSCORES"
    IN redis
    USING redis
    WITH {
        start: 0,
        stop: 9
    }
```

The first `redis` query lazily loads and caches the server's `COMMAND` metadata. Unknown commands, metadata failures, and commands without the read-only flag are rejected before execution. The account therefore needs permission to execute `COMMAND` for this dialect.

Use `redis_exec` for mutations or when a compatible server denies command metadata access:

```fql
LET result = QUERY ONE "SET session:$id $value EX $ttl NX"
    IN redis
    USING redis_exec
    WITH {
        id: sessionId,
        value: token,
        ttl: 300
    }
```

Redis modifiers and subcommands remain literal parts of the query template. There are no command-specific wrappers or option objects.

Hash writes remain generic:

```fql
LET added = QUERY ONE "HSET user:$id name $name active $active"
    IN redis
    USING redis_exec
    WITH {
        id: 42,
        name: "Tim",
        active: true
    }
```

Finite Stream commands also work through the same request/response path:

```fql
LET eventID = QUERY ONE "XADD events:$tenant * type $type payload $payload"
    IN redis
    USING redis_exec
    WITH {
        tenant: "acme",
        type: "created",
        payload: data
    }
```

## Literal Quoting and Dollar Signs

Unquoted ASCII whitespace separates Redis arguments. Single- or double-quoted Redis literals preserve whitespace and may be empty. Because the Ferret query expression is itself a string, use an outer single-quoted FQL string when the Redis template needs double quotes:

```fql
LET result = QUERY ONE 'SET greeting "hello world"'
    IN redis
    USING redis_exec
```

Double-quoted Redis literals support `\"`, `\\`, `\$`, `\n`, `\r`, `\t`, `\b`, `\a`, and `\xHH`. Single-quoted Redis literals support `\'`, `\\`, and `\$`. Quotes must wrap the complete Redis argument; malformed quotes and escapes fail before execution.

Placeholders remain active inside quoted literals. Quoting controls argument boundaries but does not turn a standalone typed binding into a string or disable a complete spread placeholder.

Only `$` followed by an ASCII identifier starts a binding. Bare `$`, `$.path`, and `$[0]` remain literal, which keeps RedisJSON paths natural:

```fql
LET document = QUERY ONE "JSON.GET document:$id $.profile"
    IN redis
    USING redis
    WITH {
        id: 42
    }
```

Use `\$name` in the Redis template when an identifier-shaped dollar token must remain literal.

## Execution Options

Generic execution controls stay in Ferret's `OPTIONS` clause:

```fql
LET value = QUERY ONE "GET user:$id"
    IN redis
    USING redis
    WITH {
        id: 42
    }
    OPTIONS {
        timeout: "2s"
    }
```

`OPTIONS.timeout` accepts a Ferret duration, a duration string, or numeric milliseconds. Zero adds no query deadline; negative durations and unknown option fields are rejected. The timeout applies to command metadata lookup and Redis execution.

## Result Values

Redis nil becomes `NONE`. Strings, integers, doubles, and Booleans become matching Ferret scalars; RESP arrays and sets become Arrays; RESP3 maps with string keys become Objects. Nested aggregates are converted recursively, while Redis errors—including nested errors—surface as Ferret errors.

A top-level Redis array is the `QUERY` result list. Scalar, Object, and `NONE` responses become a one-item result list, so use `QUERY ONE` for scalar or object responses such as GET, SET, HGETALL under RESP3, INCR, and DEL.

## Stateful Redis Features

The v1 module sends one command at a time through a pooled go-redis client. It does not define pipeline, transaction, Pub/Sub, or long-lived Stream-consumer abstractions, and connection-scoped command sequences have no same-connection guarantee.

Ordinary finite request/response commands remain generic pass-through. Give blocking commands an explicit timeout; Pub/Sub and blocking event-consumer integration remain future work.

## Testing

Unit tests do not require Redis. Live integration tests run when `REDIS_URL` is set:

```sh
REDIS_URL='redis://localhost:6379/0?protocol=3' \
GOWORK=off go test ./...
```

GitHub Actions runs the same live suite against Redis 8.8.
