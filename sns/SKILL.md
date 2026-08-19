---
name: sns
description: Use the github.com/cccteam/ccc/sns package when an HTTP endpoint receives Amazon SNS messages (HTTPS subscriptions, webhook notifications) and must verify the message signature is authentically from AWS SNS before processing. Reach for it whenever handling SubscriptionConfirmation/Notification payloads, validating SigningCertURL, or any inbound-SNS webhook work. It verifies inbound messages only — it does NOT publish to SNS.
---

# sns

Receive-side verification for Amazon SNS. Despite the name, **this package does not
publish anything** and has no AWS SDK dependency: it verifies that an inbound HTTP
request body is an authentic SNS message per AWS's signature-verification
guidelines, and returns the decoded payload. Use it in the handler behind an SNS
HTTPS subscription endpoint.

## API

```go
type Payload struct {
    Message, MessageID, Signature, SignatureVersion,
    SigningCertURL, SubscribeURL, Subject, Timestamp,
    Token, TopicArn, Type, UnsubscribeURL string
}

func New() *Client
func (c *Client) VerifyAuthenticity(ctx context.Context, reqBody io.Reader) (*Payload, error)
```

`Client` is stateless and safe to share across goroutines.

## Usage

```go
snsClient := sns.New()

func handler(w http.ResponseWriter, r *http.Request) {
    payload, err := snsClient.VerifyAuthenticity(r.Context(), r.Body)
    if err != nil {
        http.Error(w, "invalid SNS message", http.StatusForbidden)
        return
    }

    // YOUR responsibility — the library does not validate TopicArn:
    if payload.TopicArn != expectedTopicARN {
        http.Error(w, "unexpected topic", http.StatusForbidden)
        return
    }

    switch payload.Type {
    case "SubscriptionConfirmation":
        // GET payload.SubscribeURL to confirm the subscription
    case "Notification":
        process(payload.Message)
    case "UnsubscribeConfirmation":
        // ...
    }
}
```

## What it verifies

1. `SigningCertURL` must be `https` and its host must match
   `sns.<region>.amazonaws.com` (or `.com.cn` for AWS China).
2. Fetches the signing certificate (10s-timeout HTTP client, context-aware).
3. Checks the signature over the canonical `"Key\nValue\n"` string of non-empty
   fields. `SignatureVersion == "2"` → SHA256-RSA; anything else → SHA1-RSA.

## Caller responsibilities & gotchas

- **`TopicArn` is NOT validated** — always compare it yourself, or you accept
  messages from any topic in any AWS account.
- **No replay protection** — there is no `Timestamp` freshness check; add one if
  you need it.
- Whitelist the `SigningCertURL` host to your active regions for extra rigor
  (the built-in regex accepts any region).
- A non-nil `*Payload` can be returned **alongside an error** (populated as far
  as decoding got) — check `err` first; non-nil payload ≠ verified.
- Every call re-fetches the certificate over the network (no caching); consider
  caching upstream for high-volume endpoints.
- No AWS credentials, region config, or env vars required — the only external
  dependency is the HTTPS GET for the certificate.
