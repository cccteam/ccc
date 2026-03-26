// Package middleware contains middleware functions for authentication and authorization.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"cloud.google.com/go/auth/credentials/idtoken"
	"github.com/cccteam/ccc/tracer"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type AudienceOption int

const (
	AudienceHostOnly AudienceOption = iota // e.g., "example.com"
	AudienceHostURL                        // e.g., "https://example.com"
	AudienceFullURL                        // e.g., "https://example.com/path"
)

// RequireGoogleServiceAccount is a middleware that verifies incoming HTTP requests
// are authenticated by a specific Google Service Account.
//
// It extracts the OIDC token from the "Authorization: Bearer" header and validates it using
// Google's public certificates. The validation ensures that:
// 1. The token is properly signed and not expired.
// 2. The token's audience matches the specified AudienceOption (based on the request URL/host).
// 3. The token contains a verified email claim.
// 4. The verified email exactly matches the expectedEmail parameter.
//
// If validation fails at any step, the middleware intercepts the request and returns an
// HTTP 401 Unauthorized response. Otherwise, it delegates to the next handler in the chain.
func RequireGoogleServiceAccount(expectedEmail string, audienceOption AudienceOption) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
			ctx, span := tracer.Start(r.Context())
			defer span.End()

			// Verify the request has an authorization header
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				return httpio.NewEncoder(w).UnauthorizedMessage(ctx, "invalid Authorization header")
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")

			var audience string
			scheme := "https"
			if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
				scheme = "http"
			}

			switch audienceOption {
			case AudienceFullURL:
				audience = scheme + "://" + r.Host + r.URL.Path
			case AudienceHostURL:
				audience = scheme + "://" + r.Host
			case AudienceHostOnly:
				fallthrough
			default:
				audience = r.Host
			}

			payload, err := idtoken.Validate(r.Context(), token, audience)
			if err != nil {
				return httpio.NewEncoder(w).UnauthorizedMessageWithError(ctx, err, "invalid token")
			}

			// Verify the request is coming from the expected service account email
			email, err := verifiedEmail(ctx, payload)
			if err != nil {
				return httpio.NewEncoder(w).UnauthorizedMessageWithError(ctx, err, "invalid token")
			}

			if email != expectedEmail {
				return httpio.NewEncoder(w).UnauthorizedMessageWithError(ctx, errors.New("unauthorized email"), "invalid token")
			}

			next.ServeHTTP(w, r)

			return nil
		})
	}
}

func verifiedEmail(ctx context.Context, payload *idtoken.Payload) (string, error) {
	_, span := tracer.Start(ctx)
	defer span.End()

	emailVerified, emailVerifiedFound := payload.Claims["email_verified"]
	if !emailVerifiedFound {
		return "", errors.New("email_verified not found in token")
	}

	emailVerifiedBool, isBool := emailVerified.(bool)
	if !isBool {
		return "", errors.New("email_verified is not a boolean")
	}

	if !emailVerifiedBool {
		return "", errors.New("email is not verified")
	}

	email, emailFound := payload.Claims["email"]
	if !emailFound {
		return "", errors.New("email not found in token")
	}

	emailStr, isStr := email.(string)
	if !isStr {
		return "", errors.New("email is not a string")
	}

	return emailStr, nil
}
