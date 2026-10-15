// principal.go names the account a command runs as: the application-default credentials'
// principal, the account the cloud's audit logs record, read from the token the command
// already holds.

package deploy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2/google"
)

const (
	// emailScope is the scope under which the token endpoint names the account.
	emailScope = "https://www.googleapis.com/auth/userinfo.email"
	// userinfoURL answers the account behind a token.
	userinfoURL = "https://www.googleapis.com/oauth2/v3/userinfo"
)

// Principal is the account the application-default credentials sign as: a person's
// account after gcloud auth application-default login, or a service account's. An error
// is no credentials, or an endpoint that names none.
func Principal(ctx context.Context) (string, error) {
	client, err := google.DefaultClient(ctx, emailScope)
	if err != nil {
		return "", errors.Wrap(err, "google.DefaultClient()")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userinfoURL, http.NoBody)
	if err != nil {
		return "", errors.Wrap(err, "http.NewRequestWithContext()")
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "http.Client.Do(): userinfo")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", errors.Wrap(err, "io.ReadAll(): userinfo")
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.Newf("userinfo: %s", resp.Status)
	}
	var info struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", errors.Wrap(err, "json.Unmarshal(): userinfo")
	}
	if info.Email == "" {
		return "", errors.New("userinfo names no account")
	}

	return info.Email, nil
}
