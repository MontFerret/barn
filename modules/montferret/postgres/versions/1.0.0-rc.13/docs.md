# DB::POSTGRES Module

`github.com/MontFerret/contrib/modules/db/postgres` registers Postgres lifecycle helpers under the `DB::POSTGRES` namespace for Ferret hosts.

The namespace exposes only lifecycle functions:

- `DB::POSTGRES::OPEN`
- `DB::POSTGRES::CLOSE`
- `DB::POSTGRES::BEGIN`
- `DB::POSTGRES::COMMIT`
- `DB::POSTGRES::ROLLBACK`

Database and transaction handles are opaque host values that support:

- `Queryable`
- runtime cleanup through `Close`

## Install

```sh
go get github.com/MontFerret/contrib/modules/db/postgres
```

## Register The Module

```go
package main

import (
	"github.com/MontFerret/ferret/v2"

	postgresmodule "github.com/MontFerret/contrib/modules/db/postgres"
)

func main() {
	engine, err := ferret.New(
		ferret.WithModules(postgresmodule.New()),
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
| `DB::POSTGRES::OPEN` | `OPEN(options)` | Postgres DB handle | Opens a database from a connection URI or structured connection fields. |
| `DB::POSTGRES::CLOSE` | `CLOSE(db)` | `Boolean` | Closes a DB handle. Repeated close is idempotent. |
| `DB::POSTGRES::BEGIN` | `BEGIN(db)` | Postgres transaction handle | Starts an explicit transaction. |
| `DB::POSTGRES::COMMIT` | `COMMIT(tx)` | `Boolean` | Commits a transaction and finishes the handle. |
| `DB::POSTGRES::ROLLBACK` | `ROLLBACK(tx)` | `Boolean` | Rolls back a transaction and finishes the handle. |

## Opening Databases

Exactly one connection source must be provided.

Use `uri` for a Postgres URL or pgx-compatible connection string:

```fql
LET db = DB::POSTGRES::OPEN({
  uri: "postgres://ferret:secret@localhost:5432/ferret?sslmode=disable"
})
```

Or use structured fields:

```fql
LET db = DB::POSTGRES::OPEN({
  host: "localhost",
  port: 5432,
  database: "ferret",
  user: "ferret",
  password: "secret",
  sslMode: "disable"
})
```

For structured options, `host`, `database`, and `user` are required. `port` defaults to `5432`. `password` and `sslMode` are optional.

## Querying

Use `QUERY` when SQL returns rows.

```fql
LET rows = QUERY `
  SELECT id, name
  FROM users
` IN db USING sql
```

Parameterized queries use Postgres `$1`, `$2`, and later placeholders:

```fql
LET rows = QUERY `
  SELECT id, name
  FROM users
  WHERE id = $1
` IN db USING sql WITH {
  params: [1]
}
```

`QUERY ONE`, `QUERY EXISTS`, and `QUERY COUNT` are supported. `QUERY COUNT` counts rows produced by the caller's SQL; it does not rewrite the query.

## Executing Commands

Use `QUERY ONE ... USING sql_exec` when SQL should execute as a command and return execution metadata. The `sql_exec` dialect always uses Postgres' exec path and does not scan rows, even if the SQL includes `RETURNING`.

```fql
LET result = QUERY ONE `
  UPDATE users
  SET name = $1
  WHERE id = $2
` IN db USING sql_exec WITH {
  params: ["Ada", 1]
}

RETURN result
```

Result shape:

```json
{
  "rowsAffected": 1,
  "lastInsertId": null
}
```

Postgres does not expose a portable `LastInsertId` through `database/sql`, so `lastInsertId` is always `NONE`. Use `RETURNING` with `USING sql` when generated values are needed:

```fql
LET rows = QUERY `
  INSERT INTO users(name)
  VALUES ($1)
  RETURNING id
` IN db USING sql WITH {
  params: ["Ada"]
}
```

The base `QUERY` form returns the same metadata object in a single-item array. `QUERY COUNT ... USING sql_exec` returns `1` after a successful execution, and `QUERY EXISTS ... USING sql_exec` returns `true`.

## Example

```fql
USE DB::POSTGRES AS postgres

LET db = postgres::OPEN({
  uri: "postgres://ferret:secret@localhost:5432/ferret?sslmode=disable"
})

LET create = QUERY ONE `
  CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL
  )
` IN db USING sql_exec

LET inserted = QUERY ONE `
  INSERT INTO users(name)
  VALUES ($1)
  RETURNING id, name
` IN db USING sql WITH {
  params: ["Ada"]
}

RETURN inserted
```

## Transactions

```fql
LET db = DB::POSTGRES::OPEN({ uri: "postgres://ferret:secret@localhost:5432/ferret?sslmode=disable" })
LET tx = DB::POSTGRES::BEGIN(db)

LET insert = QUERY ONE `
  INSERT INTO users(name)
  VALUES ($1)
` IN tx USING sql_exec WITH {
  params: ["Ada"]
}

DB::POSTGRES::COMMIT(tx)
```

Active transactions roll back during cleanup if they are not committed or rolled back explicitly. Using a finished transaction returns a runtime error.

## Integration Testing

Module unit tests do not require a running Postgres server. Live integration tests run only when `POSTGRES_DSN` is set:

```sh
POSTGRES_DSN='postgres://ferret:secret@localhost:5432/ferret?sslmode=disable' \
  go test ./...
```

GitHub Actions runs the same live test path with a Postgres service container in `.github/workflows/integration.yml`.

## Behavior Notes

- SQL is never rewritten and `LIMIT` is never appended automatically.
- SQL `NULL` maps to Ferret `NONE`.
- Postgres integer, float, string, boolean, binary, and timestamp driver values map to Ferret `Int`, `Float`, `String`, `Boolean`, `Binary`, and `DateTime`.
- Ferret `DateTime` values can be used as query parameters.
- Handles are opaque host resources and serialize as debug strings such as `<db.postgres.connection>`.
- Errors include `DB::POSTGRES`, the operation, and the underlying Postgres or validation cause.
