# OAuth 2.0

The OAuth 2.0 module provides machine-to-machine token acquisition and token
lifecycle helpers under `SECURITY::OAUTH2`.

It supports:

- OAuth authorization-server metadata discovery;
- manually configured providers;
- `client_credentials` grants;
- refresh-token grants;
- absolute-URI extension grants;
- safe token inspection; and
- Bearer authorization-header generation.

The module is an OAuth client, not an authorization server. Browser
authorization flows, authorization code, PKCE, implicit, resource-owner
password credentials, device authorization, OpenID Connect validation, token
persistence, revocation and introspection requests, DPoP, mutual TLS, and
`private_key_jwt` are outside this version's scope.

## Complete machine-to-machine example

Use aliases when importing module namespaces:

```fql
USE SECURITY::OAUTH2 AS oauth2
USE NET::REST AS rest

LET provider = oauth2::DISCOVER(@issuer, {
  timeout: 10000
})
LET client = oauth2::CLIENT(provider, {
  clientID: @clientID,
  clientSecret: @clientSecret
})
LET token = oauth2::CLIENT_CREDENTIALS(client, {
  scope: ["inventory:read"]
})
LET api = rest::CLIENT({
  baseUrl: @apiURL,
  headers: oauth2::AUTH_HEADER(token)
})

RETURN QUERY "/inventory" IN api
```

All option durations are non-negative integer milliseconds. For example,
`timeout: 10000` means ten seconds and `skew: 30000` means thirty seconds.

## Providers

### Discovery

`DISCOVER(issuer, options?)` loads RFC 8414 OAuth authorization-server
metadata:

```fql
LET provider = SECURITY::OAUTH2::DISCOVER("https://auth.example.com/tenant", {
  timeout: 10000
})
```

The discovery URL inserts `/.well-known/oauth-authorization-server` before any
issuer path. The example above therefore discovers:

```text
https://auth.example.com/.well-known/oauth-authorization-server/tenant
```

Discovery requires an HTTP `200` JSON response, an `issuer` that exactly equals
the requested issuer, and a valid `token_endpoint`. Metadata URLs must be
absolute HTTP(S) URLs with no user information or fragment. HTTPS is required
unless `insecureAllowHTTP: true` is explicitly set.

When discovery metadata omits
`token_endpoint_auth_methods_supported`, the RFC 8414 default is
`client_secret_basic`. Other omitted discovery metadata uses the applicable
RFC defaults.

### Manual configuration

`PROVIDER(config)` creates a provider without network discovery:

```fql
LET provider = SECURITY::OAUTH2::PROVIDER({
  issuer: "https://auth.example.com",
  authorizationEndpoint: "https://auth.example.com/oauth/authorize",
  tokenEndpoint: "https://auth.example.com/oauth/token",
  revocationEndpoint: "https://auth.example.com/oauth/revoke",
  introspectionEndpoint: "https://auth.example.com/oauth/introspect",
  jwksURI: "https://auth.example.com/.well-known/jwks.json",
  scopesSupported: ["users:read"],
  grantTypesSupported: ["client_credentials", "refresh_token"],
  tokenEndpointAuthMethodsSupported: ["client_secret_basic"]
})
```

`tokenEndpoint` is required. Every supplied URL is validated with the same
absolute HTTP(S), no-userinfo, no-fragment, and HTTPS-by-default rules as
discovery. A token endpoint containing sensitive query keys such as
`access_token`, `client_secret`, `refresh_token`, `id_token`, `assertion`,
`code`, or `code_verifier` is rejected.

Unlike discovered metadata, omitted metadata on a manual provider remains
unknown. In particular, omitting
`tokenEndpointAuthMethodsSupported` does not constrain the client's supported
authentication method.

For local development, HTTP URL validation can be enabled explicitly:

```fql
LET provider = SECURITY::OAUTH2::PROVIDER({
  tokenEndpoint: "http://127.0.0.1:8080/oauth/token",
  insecureAllowHTTP: true
})
```

`insecureAllowHTTP` only permits URL validation. It never weakens Ferret's
outbound network policy, so localhost and private-network requests still
require the host to allow them.

Provider values expose these safe properties:

- `issuer`
- `authorizationEndpoint`
- `tokenEndpoint`
- `revocationEndpoint`
- `introspectionEndpoint`
- `jwksURI`
- `scopesSupported`
- `grantTypesSupported`
- `tokenEndpointAuthMethodsSupported`

## Clients

`CLIENT(provider, config)` creates an OAuth client:

```fql
LET client = SECURITY::OAUTH2::CLIENT(provider, {
  clientID: @clientID,
  clientSecret: @clientSecret,
  authMethod: "client_secret_basic"
})
```

Supported token-endpoint authentication methods are:

| Method | Behavior |
| --- | --- |
| `client_secret_basic` | Sends form-encoded client credentials through HTTP Basic authentication. |
| `client_secret_post` | Sends `client_id` and `client_secret` in the form body. |
| `none` | Sends only `client_id`; intended for public-client-compatible grants. |

When `authMethod` is omitted, clients with a secret default to
`client_secret_basic`; clients without a secret default to `none`.
`client_secret_basic` and `client_secret_post` require a secret, while `none`
rejects one. If provider metadata advertises authentication methods, the
selected method must be present in that list.

The `client_credentials` grant requires an authenticated confidential client,
so it rejects `none`. Basic authentication percent-encodes both the client ID
and secret as form values before Base64 encoding, as required by RFC 6749.

Client values expose their provider, `clientID`, `authMethod`, and
`hasClientSecret`. They never expose `clientSecret`.

## Client credentials

`CLIENT_CREDENTIALS(client, options?)` requests a token with
`grant_type=client_credentials`:

```fql
LET token = SECURITY::OAUTH2::CLIENT_CREDENTIALS(client, {
  scope: ["users:read", "projects:read"],
  audience: "https://api.example.com",
  parameters: {
    resource: [
      "https://api.example.com",
      "https://uploads.example.com"
    ]
  },
  timeout: 10000
})
```

`scope` accepts a space-separated string or an array of strings. Parameters
accept scalar values or scalar arrays; arrays are encoded as repeated form
fields and `NONE` values are omitted. Nested objects are rejected.

Client authentication fields are always reserved, including `client_id`,
`client_secret`, `client_assertion`, and `client_assertion_type`. Client
credentials also reserves `grant_type`, `scope`, and `audience`; these fields
cannot be replaced through `parameters`.

## Refreshing a token

`REFRESH(client, tokenOrOptions)` can refresh an existing token set:

```fql
LET refreshed = SECURITY::OAUTH2::REFRESH(client, token)
```

It can also accept an explicit refresh-token options object:

```fql
LET refreshed = SECURITY::OAUTH2::REFRESH(client, {
  refreshToken: @refreshToken,
  scope: ["users:read"],
  parameters: {
    resource: "https://api.example.com"
  },
  timeout: 10000
})
```

A refresh token is required. If the server omits `refresh_token`, the returned
token set retains the previous value; a non-empty rotated token replaces it.
When a response omits `scope`, the effective requested or previous scope is
preserved.

Refresh requests reserve all client authentication fields plus `grant_type`,
`refresh_token`, and `scope`.

## Extension grants

`TOKEN(client, parameters)` is the advanced escape hatch for token-endpoint
extension grants:

```fql
LET token = SECURITY::OAUTH2::TOKEN(client, {
  grant_type: "urn:ietf:params:oauth:grant-type:jwt-bearer",
  assertion: @assertion,
  scope: "users:read"
})
```

`grant_type` is required exactly once and must be an absolute URI. Client
authentication parameters remain reserved. An `assertion` extension parameter
is allowed and treated as secret.

## Token values and helpers

Token-set properties intentionally expose only safe inspection data:

- `tokenType`
- `scopes`
- `expiresAt`
- `expired`
- `validFor`
- `hasRefreshToken`
- `hasIDToken`

Unknown expiration is represented as `NONE` by `expiresAt` and `validFor`.
`expired` is `false` when expiration is unknown. `validFor` is the remaining
integer milliseconds, rounded up and clamped to zero.

The helper functions are:

| Function | Result |
| --- | --- |
| `ACCESS_TOKEN(token)` | Raw access-token string. |
| `REFRESH_TOKEN(token)` | Raw refresh-token string, or `NONE`. |
| `ID_TOKEN(token)` | Raw ID-token string, or `NONE`. No parsing or validation is performed. |
| `TOKEN_TYPE(token)` | Token-type string returned by the provider. |
| `SCOPES(token)` | Scope strings split on ASCII spaces. |
| `EXPIRES_AT(token)` | Ferret timestamp, or `NONE` when unknown. |
| `EXPIRED(token, options?)` | Whether the token is expired, optionally applying a millisecond `skew`. |
| `VALID_FOR(token)` | Remaining integer milliseconds, or `NONE` when unknown. |
| `AUTH_HEADER(token)` | `{ Authorization: "Bearer <access-token>" }`. |

For example:

```fql
LET shouldRefresh = SECURITY::OAUTH2::EXPIRED(token, {
  skew: 30000
})

RETURN {
  tokenType: SECURITY::OAUTH2::TOKEN_TYPE(token),
  scopes: SECURITY::OAUTH2::SCOPES(token),
  expiresAt: SECURITY::OAUTH2::EXPIRES_AT(token),
  shouldRefresh: shouldRefresh
}
```

`AUTH_HEADER` accepts only case-insensitive `Bearer` token types and always
emits the canonical `Bearer` scheme. It validates the OAuth Bearer token
grammar before producing the header, preventing control-character and header
injection. Other token types remain inspectable but cannot use this helper.

## Secret handling

Provider, client, and token values are immutable host values with
secret-safe formatting and serialization. Normal string formatting, Go and
Ferret JSON, and MessagePack never reveal client secrets or raw tokens.
Present token fields serialize as `"<redacted>"`; empty token fields are
omitted. Provider-specific response fields are retained internally but excluded
from property access and default serialization because unknown fields may
themselves contain credentials.

The following functions intentionally materialize secrets:

```fql
SECURITY::OAUTH2::ACCESS_TOKEN(token)
SECURITY::OAUTH2::REFRESH_TOKEN(token)
SECURITY::OAUTH2::ID_TOKEN(token)
SECURITY::OAUTH2::AUTH_HEADER(token)
```

Do not log or return those results unnecessarily. `AUTH_HEADER` is intended to
be passed directly to an HTTP client configuration.

## HTTP and error behavior

Discovery and token requests use the policy-aware HTTP client supplied by the
Ferret execution context. Requests therefore retain the host's SSRF, allowed
host, redirect, timeout, request-size, response-size, and cancellation
controls. A shorter per-operation `timeout` may be supplied, but it cannot
weaken those controls. Network and policy errors remain available in their
original error chains.

Token requests are POST form bodies with `Accept: application/json`.
Credentials and access tokens are never added to URI queries. For
`client_secret_post` and `none`, the module suppresses any ambient default
`Authorization` header so the request cannot accidentally use two
authentication methods.

Redirect handling is governed by the host's global Ferret HTTP policy. When
credential forwarding across redirects must be prohibited, disable redirects
or restrict the allowed destination hosts in that policy.

Only HTTP `200` JSON token responses are accepted as success. Successful
responses require non-empty `access_token` and `token_type`. Integral,
non-negative `expires_in` values are converted to an expiration timestamp.
Unknown response fields are retained without losing JSON-number precision.

Standard OAuth failures are returned as typed OAuth errors containing the
operation, OAuth error code, sanitized description, error URI, and status.
Submitted secrets and raw request or response bodies are never retained in
errors.
