# sns

The `sns` package provides tooling to verify the authenticity of an Amazon SNS (Simple Notification Service) Payload. The validation process adheres to the guidelines outlined in the [AWS Documentation](https://docs.aws.amazon.com/sns/latest/dg/sns-verify-signature-of-message.html).

Despite the name, **this package does not publish anything** and has no AWS SDK
dependency: it is receive-side only. Use it in the handler behind an SNS HTTPS
subscription endpoint to verify an inbound message really came from AWS before
processing it.

### Key Features

- Ensures the `SigningCertURL` originates from a valid Amazon SNS domain.
- Supports both the standard AWS regions (`https://sns.<your-region>.amazonaws.com`) and the AWS China regions (`https://sns.<your-region>.amazonaws.com.cn`).

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

    // The caller's responsibility — the library does not validate TopicArn:
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

### Important Notes

**1. TopicArn Validation:**
This library does **NOT** perform validation on the `TopicArn`. Users of this library are responsible for handling this validation on their own — otherwise the endpoint accepts messages from any SNS topic in any AWS account.

**2. SigningCertURL Whitelisting:**
For added security, it is highly recommended to whitelist the `SigningCertURL` host against the specific AWS regions you're actively using (e.g., `https://sns.us-west-1.amazonaws.com`). This ensures that the certificate URL matches the intended region (the built-in regex accepts any region).

**3. No replay protection:**
There is no `Timestamp` freshness check; add one if you need it.

**4. Error handling:**
A non-nil `*Payload` can be returned **alongside an error** (populated as far as
decoding got) — check `err` first; a non-nil payload does not mean "verified".

**5. Certificate fetching:**
Every call re-fetches the signing certificate over the network (no caching);
consider caching upstream for high-volume endpoints. No AWS credentials, region
config, or environment variables are required — the only external dependency is
the HTTPS GET for the certificate.
