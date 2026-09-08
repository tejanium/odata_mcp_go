# OAuth 2.0 Authentication

The bridge supports the OAuth 2.0 client credentials grant (RFC 6749 section 4.4)
for services that require a bearer token rather than basic auth or cookies.

Tokens are fetched on demand, cached in memory, and refreshed before they expire.
Nothing is written to disk.

## Configuration

Any of these five settings can come from a flag or the environment.

| Flag | Environment | Required | Default |
|---|---|---|---|
| `--oauth-client-id` | `OAUTH_CLIENT_ID`, `ODATA_OAUTH_CLIENT_ID` | yes | |
| `--oauth-client-secret` | `OAUTH_CLIENT_SECRET`, `ODATA_OAUTH_CLIENT_SECRET` | yes | |
| `--oauth-token-url` | `OAUTH_TOKEN_URL`, `ODATA_OAUTH_TOKEN_URL` | yes | |
| `--oauth-scope` | `OAUTH_SCOPE`, `ODATA_OAUTH_SCOPE` | no | none sent |
| `--oauth-client-auth` | `OAUTH_CLIENT_AUTH`, `ODATA_OAUTH_CLIENT_AUTH` | no | `basic` |

Flags win over the environment, and the bare `OAUTH_*` names win over the
`ODATA_OAUTH_*` ones. The bare names match the Python implementation, so an
existing `.env` works unchanged.

## Usage

```bash
odata-mcp \
  --oauth-client-id "$CLIENT_ID" \
  --oauth-client-secret "$CLIENT_SECRET" \
  --oauth-token-url https://auth.example.com/oauth/token \
  --oauth-scope "https://graph.microsoft.com/.default" \
  https://services.example.com/odata/
```

Or entirely from the environment:

```bash
export OAUTH_CLIENT_ID=... OAUTH_CLIENT_SECRET=... OAUTH_TOKEN_URL=...
odata-mcp https://services.example.com/odata/
```

## Client authentication method

`--oauth-client-auth` controls how the credentials reach the token endpoint:

- `basic` (default) puts them in an HTTP Basic header. RFC 6749 section 2.3.1
  requires every authorization server to support this.
- `body` puts them in the form body as `client_id` and `client_secret`. That
  section makes it optional, but some servers accept only this form.

Start with the default and switch to `body` if the token endpoint returns 401
for credentials you know are correct.

## Token lifecycle

A token is reused until it approaches expiry, at which point the next request
fetches a fresh one. The margin is a fifth of the token's lifetime, clamped
between 5 and 60 seconds, so a long request cannot begin on a token that dies
mid-flight and a short-lived token is not treated as stale the moment it
arrives. Services that omit `expires_in` are assumed to issue one-hour tokens.

Concurrent requests arriving on an expired token share a single fetch rather
than each hitting the authorization server.

## Precedence and exclusivity

Authentication is chosen in this order: OAuth, then cookies, then basic auth,
then anonymous. Only one may be configured; combining OAuth with `--user`,
`--cookie-file` or `--cookie-string` is an error.

Setting some but not all of the required OAuth values is also an error, and it
names what is missing. This is deliberate: a partial configuration silently
falling back to anonymous access produces a confusing 401 from the OData
service instead of a clear message.

## Verifying

`--trace -v` prints the token exchange and the resolved authentication mode
without starting the server:

```
[VERBOSE] Using OAuth 2.0 client credentials, token URL: https://auth.example.com/oauth/token
[VERBOSE] Fetching OAuth token from https://auth.example.com/oauth/token
[VERBOSE] OAuth token obtained, expires in 300 seconds
[VERBOSE] Using configured OAuth bearer token
  "authentication": "OAuth 2.0 client credentials (client: my-client-id)",
```

## Troubleshooting

**401 from the token endpoint.** Check the credentials, then try
`--oauth-client-auth body`.

**401 from the OData service with a token that was issued.** The scope is
probably wrong or missing. Many services namespace scopes as URLs and need an
exact match.

**`OAuth requires client secret, token URL`.** Only part of the configuration
was found. Remember that flags override the environment, so an empty flag value
does not fall back.

Client secrets never appear in error messages or verbose output. Failed token
responses are quoted back truncated, so an authorization server that echoes a
request cannot leak a credential through the log.
