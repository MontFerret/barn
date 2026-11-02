# SECURITY::JWT Module

`github.com/MontFerret/contrib/modules/security/jwt` registers JWT signing,
verification, inspection, and key helpers under the `SECURITY::JWT` namespace.

The module exposes these functions:

- `SECURITY::JWT::INSPECT`
- `SECURITY::JWT::VERIFY`
- `SECURITY::JWT::SIGN`
- `SECURITY::JWT::HMAC_KEY`
- `SECURITY::JWT::PUBLIC_KEY`
- `SECURITY::JWT::PRIVATE_KEY`

## Install

```sh
go get github.com/MontFerret/contrib/modules/security/jwt
```

## Register The Module

```go
package main

import (
    "github.com/MontFerret/ferret/v2"

    jwtmodule "github.com/MontFerret/contrib/modules/security/jwt"
)

func main() {
    engine, err := ferret.New(
        ferret.WithModules(jwtmodule.New()),
    )
    if err != nil {
        panic(err)
    }

    _ = engine
}
```

`jwtmodule.WithMaxTokenSize` changes the maximum accepted compact JWT size
from its 64 KiB default.

## HMAC Example

```fql
LET key = SECURITY::JWT::HMAC_KEY(@secret)
LET token = SECURITY::JWT::SIGN({ sub: "user-123" }, key, {
    algorithm: "HS256",
    expires_in: 3600,
    issued_at: true
})

RETURN SECURITY::JWT::VERIFY(token, key, {
    algorithms: ["HS256"],
    required: ["sub"]
})
```

`INSPECT(token)` decodes a token without establishing trust. Use
`VERIFY(token, key, options)` when claims must be authenticated. HMAC secrets
and PEM-encoded RSA, ECDSA, or Ed25519 keys are converted into opaque key
handles before signing or verification.
