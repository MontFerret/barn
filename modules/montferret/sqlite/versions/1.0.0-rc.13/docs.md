# DB::SQLITE Module

`github.com/MontFerret/contrib/modules/db/sqlite` registers SQLite lifecycle helpers under the `DB::SQLITE` namespace for Ferret hosts.

The namespace exposes only lifecycle functions:

- `DB::SQLITE::OPEN`
- `DB::SQLITE::CLOSE`
- `DB::SQLITE::BEGIN`
- `DB::SQLITE::COMMIT`
- `DB::SQLITE::ROLLBACK`

Database and transaction handles are opaque host values that support:

- `Queryable`
- runtime cleanup through `Close`

## Install

```sh
go get github.com/MontFerret/contrib/modules/db/sqlite
```

## Register The Module

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	sqlitemodule "github.com/MontFerret/contrib/modules/db/sqlite"
)

func main() {
	engine, err := ferret.New(
		ferret.WithModules(sqlitemodule.New()),
	)
	if err != nil {
		panic(err)
	}

	_ = engine
}
```

Sandboxed hosts can disable file-backed databases while keeping private and shared in-memory databases:

```go
engine, err := ferret.New(
	ferret.WithModules(sqlitemodule.New(sqlitemodule.WithMemoryOnly())),
)
if err != nil {
	panic(err)
}
```

## Function Reference

| Function | Signature | Returns | Notes |
| --- | --- | --- | --- |
| `DB::SQLITE::OPEN` | `OPEN(options)` | SQLite DB handle | Opens a private memory DB, file DB, or caller-supplied SQLite URI. |
| `DB::SQLITE::CLOSE` | `CLOSE(db)` | `Boolean` | Closes a DB handle. Repeated close is idempotent. |
| `DB::SQLITE::BEGIN` | `BEGIN(db)` | SQLite transaction handle | Starts an explicit transaction. |
| `DB::SQLITE::COMMIT` | `COMMIT(tx)` | `Boolean` | Commits a transaction and finishes the handle. |
| `DB::SQLITE::ROLLBACK` | `ROLLBACK(tx)` | `Boolean` | Rolls back a transaction and finishes the handle. |

## Opening Databases

Exactly one of `memory`, `path`, or `uri` must be provided.

```fql
LET db = DB::SQLITE::OPEN({ memory: true })
```

```fql
LET db = DB::SQLITE::OPEN({ path: "./data.db" })
```

```fql
LET db = DB::SQLITE::OPEN({
  uri: "file:example?mode=memory&cache=shared"
})
```

`memory: true` uses SQLite's private `:memory:` database behavior, so separate opens are isolated. Use `uri` for shared in-memory databases; at least one connection must remain open while other connections use the shared database.

When the module is registered with `WithMemoryOnly()`, `OPEN` accepts `memory: true` and `uri` values with `mode=memory`, and rejects file paths or file-backed URIs.

`create` defaults to `true`, and `readOnly` defaults to `false`. `readOnly: true` and `create: true` together are rejected.

## Querying

Use `QUERY` when SQL returns rows.

```fql
LET rows = QUERY `
  SELECT id, name
  FROM users
` IN db USING sql
```

Parameterized queries use SQLite `?` placeholders:

```fql
LET rows = QUERY `
  SELECT id, name
  FROM users
  WHERE id = ?
` IN db USING sql WITH {
  params: [1]
}
```

`QUERY ONE`, `QUERY EXISTS`, and `QUERY COUNT` are supported. `QUERY COUNT` counts rows produced by the caller's SQL; it does not rewrite the query.

## Executing Commands

Use `QUERY ONE ... USING sql_exec` when SQL should execute as a command and return execution metadata. The `sql_exec` dialect always uses SQLite's exec path and does not scan rows, even if the SQL includes `RETURNING`.

```fql
LET result = QUERY ONE `
  INSERT INTO users(name)
  VALUES (?)
` IN db USING sql_exec WITH {
  params: ["Ada"]
}

RETURN result
```

Result shape:

```json
{
  "rowsAffected": 1,
  "lastInsertId": 1
}
```

For non-insert statements, `lastInsertId` is `NONE`.

The base `QUERY` form returns the same metadata object in a single-item array. `QUERY COUNT ... USING sql_exec` returns `1` after a successful execution, and `QUERY EXISTS ... USING sql_exec` returns `true`.

## Example

```fql
USE DB::SQLITE AS sqlite

LET db = sqlite::OPEN({ memory: true })

LET create = QUERY ONE `
  CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL
  )
` IN db USING sql_exec

LET insert = QUERY ONE `
  INSERT INTO users(name)
  VALUES (?)
` IN db USING sql_exec WITH {
  params: ["Ada"]
}

RETURN QUERY `
  SELECT id, name
  FROM users
` IN db USING sql
```

## Transactions

```fql
LET db = DB::SQLITE::OPEN({ memory: true })
LET tx = DB::SQLITE::BEGIN(db)

LET insert = QUERY ONE `
  INSERT INTO users(name)
  VALUES (?)
` IN tx USING sql_exec WITH {
  params: ["Ada"]
}

DB::SQLITE::COMMIT(tx)
```

Active transactions roll back during cleanup if they are not committed or rolled back explicitly. Using a finished transaction returns a runtime error.

## Behavior Notes

- SQL is never rewritten and `LIMIT` is never appended automatically.
- SQL `NULL` maps to Ferret `NONE`.
- SQLite integer, float, string, boolean, and blob driver values map to Ferret `Int`, `Float`, `String`, `Boolean`, and `Binary`.
- SQLite has no dedicated boolean storage class; integer results are returned as `Int` and are not guessed as booleans.
- Handles are opaque host resources and serialize as debug strings such as `<db.sqlite.connection>`.
- Errors include `DB::SQLITE`, the operation, and the underlying SQLite or validation cause.
