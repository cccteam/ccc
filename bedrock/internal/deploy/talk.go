// talk.go is how the pipeline speaks on a pull request: as the deployer GitHub App when
// the environment has one (2-env holds its App ID and the pinned version of its private
// key, which the stack passes as _DEPLOYER_APP_ID and _DEPLOYER_KEY_SECRET), minting the
// app's installation token when a step has something to say, never before and never
// kept in the workspace. A refusal the app cannot post (no deployer app yet) is posted
// with the repository's token instead; the talk-back itself is the app's alone.

package deploy

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

// The substitutions naming the deployer app, and the fact holding the repository's token.
const (
	deployerAppSub  = "_DEPLOYER_APP_ID"
	deployerKeySub  = "_DEPLOYER_KEY_SECRET"
	githubTokenFact = "GITHUB_TOKEN"
	// jwtLifetime is how long the app's JWT is good for (GitHub allows ten minutes), and
	// jwtSkew how far its issue time is set back for a clock that runs ahead.
	jwtLifetime = 9 * time.Minute
	jwtSkew     = time.Minute
)

// pullRequest is one pull request the pipeline speaks on, with the client it speaks
// through.
type pullRequest struct {
	gh          *github.Client
	owner, repo string
	number      int
	// asApp is true when the client holds the deployer app's token.
	asApp bool
}

// speaker opens the pull request of a pull-request build to speak on: as the deployer
// app when the environment has one; else, when fallback is set, with the repository's
// token (a refusal is still said); else nil, nothing to speak as. A tag build has no
// pull request and answers nil.
func speaker(ctx context.Context, clients *Clients, build *Build, env map[string]string, fallback bool) (*pullRequest, error) {
	subs := build.Substitutions
	if subs[prNumberSub] == "" {
		return nil, nil
	}
	number, err := strconv.Atoi(subs[prNumberSub])
	if err != nil || number <= 0 {
		return nil, errors.Newf("_PR_NUMBER %q is not a pull request number", subs[prNumberSub])
	}
	owner, repo, err := splitRepo(subs[repoFullNameSub])
	if err != nil {
		return nil, err
	}
	pr := &pullRequest{owner: owner, repo: repo, number: number}
	token, err := deployerToken(ctx, clients, subs, owner, repo)
	if err != nil {
		return nil, err
	}
	switch {
	case token != "":
		pr.gh, pr.asApp = clients.GitHub(token), true
	case fallback && env[githubTokenFact] != "":
		pr.gh = clients.GitHub(env[githubTokenFact])
	default:
		return nil, nil
	}

	return pr, nil
}

// comment posts on the pull request and says so.
func (pr *pullRequest) comment(ctx context.Context, body string, out io.Writer) error {
	if err := pr.gh.CreateComment(ctx, pr.owner, pr.repo, pr.number, body); err != nil {
		return errors.Wrapf(err, "posting on pull request %d", pr.number)
	}
	fmt.Fprintf(out, "Posted on pull request %d.\n", pr.number)

	return nil
}

// deployerToken mints the deployer app's installation token on the repository: the app's
// private key is read from Secret Manager by its pinned version, a JWT signed with it
// asks GitHub for the app's installation on the repository and then for a token good for
// an hour. No deployer app in the environment answers empty.
func deployerToken(ctx context.Context, clients *Clients, subs map[string]string, owner, repo string) (string, error) {
	appID, keyVersion := subs[deployerAppSub], subs[deployerKeySub]
	if appID == "" || keyVersion == "" {
		return "", nil
	}
	secrets, err := clients.Secrets(ctx)
	if err != nil {
		return "", err
	}
	defer secrets.Close()
	pemKey, err := secrets.Access(ctx, keyVersion)
	if err != nil {
		return "", errors.Wrapf(err, "the deployer app's key (%s)", keyVersion)
	}
	jwt, err := appJWT(appID, pemKey, time.Now())
	if err != nil {
		return "", err
	}
	app := clients.GitHub(jwt)
	installation, err := app.RepositoryInstallation(ctx, owner, repo)
	if err != nil {
		return "", errors.Wrapf(err, "the deployer app %s's installation on %s/%s", appID, owner, repo)
	}
	token, err := app.InstallationToken(ctx, installation)
	if err != nil {
		return "", errors.Wrapf(err, "a token for the deployer app %s's installation %d", appID, installation)
	}

	return token, nil
}

// appJWT is the JWT a GitHub App authenticates as: RS256 over the header and the claims
// (issued a minute back, good for nine, issued by the app), with the app's private key
// in PEM, PKCS#1 or PKCS#8.
func appJWT(appID string, pemKey []byte, now time.Time) (string, error) {
	block, _ := pem.Decode(pemKey)
	if block == nil {
		return "", errors.New("the deployer app's key is not PEM")
	}
	key, err := parseRSAKey(block.Bytes)
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{"iat": now.Add(-jwtSkew).Unix(), "exp": now.Add(jwtLifetime).Unix(), "iss": appID})
	if err != nil {
		return "", errors.Wrap(err, "json.Marshal()")
	}
	signed := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.Wrap(err, "rsa.SignPKCS1v15()")
	}

	return signed + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// parseRSAKey reads an RSA private key in PKCS#1 (what GitHub hands out) or PKCS#8.
func parseRSAKey(der []byte) (*rsa.PrivateKey, error) {
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, errors.New("the deployer app's key is neither a PKCS#1 nor a PKCS#8 private key")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the deployer app's key is not an RSA key")
	}

	return key, nil
}
