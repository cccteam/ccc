# middleware

Authentication middleware for `net/http`. Its primary API is
`RequireGoogleServiceAccount`, which verifies that an inbound request carries
a valid Google-issued OIDC ID token belonging to **one specific service
account**. Typical callers are Cloud Scheduler jobs, Pub/Sub push subscriptions,
Eventarc triggers, and Cloud Run service-to-service calls.

This is authentication of a single caller identity — authorization (roles,
permissions) lives elsewhere (see `accesstypes` and the `access` package).

## Usage

```go
import "github.com/cccteam/ccc/middleware"

r := chi.NewRouter()
r.With(middleware.RequireGoogleServiceAccount(
    "scheduler@my-project.iam.gserviceaccount.com",
    middleware.AudienceHostURL,
)).Post("/tasks/nightly", nightlyHandler)
```

The returned value is a standard `func(http.Handler) http.Handler`, so it works
with chi, gorilla, or plain `net/http`. On any failure it writes **HTTP 401** via
`httpio` and never calls the next handler; on success it calls `next.ServeHTTP`.

What it validates, in order:

1. Token signature (Google public certs) and expiry.
2. Token audience matches the value derived from the `AudienceOption` (below).
3. `email_verified` claim is present and `true`.
4. `email` claim exactly equals `expectedEmail` — exact string match, one identity
   per middleware instance; no lists or wildcards.

## Audience options

The middleware reconstructs the expected audience from the inbound request; it
must byte-match the audience the token was minted with:

| Option | Audience shape |
| --- | --- |
| `AudienceHostOnly` | `example.com` |
| `AudienceHostURL` | `https://example.com` |
| `AudienceFullURL` | `https://example.com/path` |

Construction details the deployment must account for:

- Scheme is `https` only when `r.TLS != nil` **or** the request carries
  `X-Forwarded-Proto: https`. Behind a TLS-terminating proxy, make sure that
  header is set, or the audience silently becomes `http://…` and every request
  401s.
- The **`APPLICATION_HOST` environment variable** overrides `r.Host` (read per
  request). Set it when a load balancer rewrites the Host header.
- `AudienceFullURL` includes `r.URL.Path`, so it requires a distinct token per
  endpoint and breaks on any path rewrite or trailing-slash difference.

## Minting a matching token (caller side)

Configure the caller (Cloud Scheduler OIDC token, Pub/Sub push auth, or
`idtoken.NewTokenSource(ctx, audience)`) with the same audience string the option
above produces, and send it as `Authorization: Bearer <token>`.

## Notes

- The `Bearer ` prefix check is **case-sensitive** — `bearer <tok>` gets a 401.
- Audience mismatch is the number-one failure mode; when debugging 401s, derive
  the audience the middleware builds (scheme + host + path per the table) and
  compare it to the token's `aud` claim.
- An unrecognized `AudienceOption` value silently falls back to
  `AudienceHostOnly` (the weakest form) — only pass the three defined constants.
- No caching beyond Google's internal cert cache: every request is fully
  validated.
- Errors are never returned to the caller and there are no sentinel errors;
  failure detail is only visible in logs/response body via `httpio`.
- Depends on `cccteam/ccc/tracer` (spans per request), `cccteam/httpio`, and
  `cloud.google.com/go/auth` — it does not import `accesstypes`.
