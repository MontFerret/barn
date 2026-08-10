# REST

The REST module provides reusable clients for REST-style and endpoint-oriented HTTP APIs under the `NET::REST` namespace.

Create a client once with a base URL, default headers, encodings, and request behavior. Then query API resources through Ferret `QUERY` expressions.

```fql
LET api = NET::REST::CLIENT({
    baseUrl: "https://api.example.com",
    headers: {
        Authorization: "Bearer " + @token
    },
    encoding: "json"
})

RETURN QUERY "/users" IN api WITH {
    query: {
        active: true,
        limit: 50
    }
} OPTIONS {
    timeout: 5000
}
```

The query string identifies the resource path. Request data is passed through `WITH`, and execution options are passed through `OPTIONS`.

## Querying Resources

A simple query sends a `GET` request to the given path:

```fql
LET api = NET::REST::CLIENT({
    baseUrl: "https://api.example.com",
    encoding: "json"
})

RETURN QUERY "/users" IN api
```

`WITH` carries request data such as query parameters, request bodies, methods, and per-request headers:

```fql
RETURN QUERY "/users" IN api WITH {
    query: {
        active: true,
        limit: 50
    }
}
```

To send a request body, provide a method and body:

```fql
RETURN QUERY "/users" IN api WITH {
    method: "POST",
    body: {
        name: "Ada",
        email: "ada@example.com"
    }
}
```

Per-request headers can also be provided through `WITH`:

```fql
RETURN QUERY "/users" IN api WITH {
    headers: {
        "X-Request-ID": @requestId
    }
}
```

Request headers are merged with the client’s default headers. Per-request headers override default headers with the same name.

## Request Options

`OPTIONS` controls request execution and response handling:

```fql
RETURN QUERY ONE "/users/1" IN api OPTIONS {
    response: "full",
    timeout: 3000,
    responseEncoding: "json"
}
```

Use `WITH` for request data. Use `OPTIONS` for execution behavior.

For example:

```fql
RETURN QUERY "/users" IN api WITH {
    query: {
        active: true,
        limit: 50
    }
} OPTIONS {
    timeout: 5000
}
```

## Query Results

Plain `QUERY` follows Ferret’s list query contract.

If a response body decodes to an array, the array items are returned as query results. If a response body decodes to an object, scalar, or binary value, it is returned as a single query result.

Use `QUERY ONE` when the endpoint represents a single response body:

```fql
RETURN QUERY ONE "/users/1" IN api
```

## Client Configuration

`NET::REST::CLIENT(config)` accepts:

| Field | Description |
| --- | --- |
| `baseUrl` | Required absolute base URL. |
| `headers` | Default request headers. |
| `encoding` | Sets both request and response encodings. |
| `requestEncoding` | Default request body encoding. |
| `responseEncoding` | Default response body encoding. |
| `timeout` | Default request timeout in milliseconds. |
| `response` | Default response mode: `"body"` or `"full"`. |
| `errorMode` | Default error mode: `"raise"` or `"response"`. |

Supported encodings are:

| Encoding | Description |
| --- | --- |
| `"json"` | Encodes and decodes JSON values. |
| `"text"` | Sends and returns plain text. |
| `"bytes"` | Sends and returns raw binary data. |
| `"form"` | Encodes request bodies as form data. |

## Response Modes

The default response mode is `"body"`.

```fql
RETURN QUERY ONE "/users/1" IN api
```

For a JSON response, this returns the decoded response body:

```fql
{
    id: 1,
    name: "Ada"
}
```

Use `response: "full"` to return HTTP response metadata together with the decoded body:

```fql
RETURN QUERY ONE "/users/1" IN api OPTIONS {
    response: "full"
}
```

Full response shape:

```fql
{
    ok: true,
    status: 200,
    headers: {
        "content-type": "application/json"
    },
    body: {
        id: 1,
        name: "Ada"
    },
    url: "https://api.example.com/users/1"
}
```

## Error Handling

By default, HTTP `4xx` and `5xx` statuses raise runtime errors.

```fql
LET api = NET::REST::CLIENT({
    baseUrl: "https://api.example.com",
    encoding: "json"
})

RETURN QUERY ONE "/users/404" IN api
```

Set `errorMode: "response"` to return failed HTTP responses as structured values:

```fql
LET api = NET::REST::CLIENT({
    baseUrl: "https://api.example.com",
    encoding: "json",
    errorMode: "response"
})

RETURN QUERY ONE "/users/404" IN api OPTIONS {
    response: "full"
}
```

The returned value has the same full response shape with `ok: false`:

```fql
{
    ok: false,
    status: 404,
    headers: {
        "content-type": "application/json"
    },
    body: {
        error: "User not found"
    },
    url: "https://api.example.com/users/404"
}
```

## Shortcut Queries

For simple requests, the query shortcut can be used:

```fql
RETURN api[~ "/users"]
```

This is equivalent to:

```fql
RETURN QUERY "/users" IN api
```

Use the full `QUERY` expression when the request needs `WITH` or `OPTIONS`.

```fql
RETURN QUERY "/users" IN api WITH {
    query: {
        active: true
    }
} OPTIONS {
    timeout: 5000
}
```

## Scope

`NET::REST` is intended for REST-style and endpoint-oriented HTTP APIs.

It is not a low-level HTTP module. Low-level HTTP primitives belong in the standard library under `IO::NET::HTTP`.