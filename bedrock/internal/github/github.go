// Package github is the small GitHub REST client bedrock needs: the organization's
// installed apps, a repository's rulesets, its refs, tags, comparisons and directory
// listings, whether it is empty, its workflows, its pull requests with their comments and deployments, and a
// GitHub App's installation token. It speaks to one API host with one token, sends a read again
// when GitHub stalls or answers a gateway error, and translates the API's refusals into errors
// that carry the status and the message.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

const (
	// DefaultBase is the public API host.
	DefaultBase = "https://api.github.com"
	// tokenEnv is the variable a token is read from before gh is asked.
	tokenEnv = "GITHUB_TOKEN"
	// apiVersion pins the REST API's shape.
	apiVersion = "2022-11-28"
	// maxBody bounds what one answer may carry.
	maxBody = 8 << 20
	// perPage is the API's largest page; commentWindow is how many of the latest
	// comments a read is sure to hold.
	perPage       = 100
	commentWindow = 5
	// requestTimeout bounds one request, from sending it to the last byte of its answer.
	requestTimeout = 30 * time.Second
	// retries is how many times a read that failed before an answer, or that GitHub
	// answered 502, 503 or 504, is sent again; retryWait is the wait before the first of
	// them, doubled before each one after.
	retries   = 2
	retryWait = 2 * time.Second
)

// Client speaks to one GitHub API host with one token.
type Client struct {
	base  string
	token string
	http  *http.Client
	wait  WaitFunc
}

// WaitFunc waits for d, or returns the context's error; a test substitutes one that does
// not wait.
type WaitFunc func(ctx context.Context, d time.Duration) error

// ClientFunc opens the client: the CLI wires Open, tests a client over a test server.
type ClientFunc func(ctx context.Context) (*Client, error)

// New is a client over the API at base, authenticated with the token. It carries a
// transport of its own, a copy of the default one, so nothing that closes the default
// transport's idle connections (a test server's close does) touches a request of its
// in flight.
func New(base, token string) *Client {
	transport, ok := http.DefaultTransport.(*http.Transport)
	var own http.RoundTripper
	if ok {
		own = transport.Clone()
	}

	return &Client{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{Timeout: requestTimeout, Transport: own}, wait: waitFor}
}

// waitFor waits for d.
func waitFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "waiting")
	case <-timer.C:
		return nil
	}
}

// Open finds a token and returns a client over the public API: GITHUB_TOKEN when set,
// else the token gh holds for its signed-in account. With neither it refuses.
func Open(ctx context.Context) (*Client, error) {
	token, err := Token(ctx)
	if err != nil {
		return nil, err
	}

	return New(DefaultBase, token), nil
}

// TokenFunc finds the token a command runs GitHub's API with.
type TokenFunc func(ctx context.Context) (string, error)

// Token is the token the account has: GITHUB_TOKEN when set, else the token gh holds for
// its signed-in account. With neither it refuses.
func Token(ctx context.Context) (string, error) {
	if token := os.Getenv(tokenEnv); token != "" {
		return token, nil
	}
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	token := strings.TrimSpace(string(out))
	if err != nil || token == "" {
		return "", errors.Newf("no GitHub token: set %s, or sign in with gh (gh auth login) so that gh auth token prints one", tokenEnv)
	}

	return token, nil
}

// Error is the API's refusal: the status it answered and the message it gave.
type Error struct {
	Status  int
	Message string
	Method  string
	Path    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("GitHub %s %s answered %d: %s", e.Method, e.Path, e.Status, e.Message)
}

// NotFound reports whether the error is the API's 404.
func NotFound(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusNotFound
	}

	return false
}

// do sends the request with the JSON body in (nil for none) and decodes the answer into
// out (nil to discard it). A status outside 2xx is an *Error. A read (GET) that fails
// before an answer (a timeout, a reset connection) or that GitHub answers 502, 503 or 504
// is sent again, up to retries times after a wait, and the last error is the one
// returned; a write is sent once, since a repeated write could duplicate what it makes.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var data []byte
	if in != nil {
		var err error
		if data, err = json.Marshal(in); err != nil {
			return errors.Wrap(err, "json.Marshal()")
		}
	}
	attempts := 1
	if method == http.MethodGet {
		attempts += retries
	}
	wait := retryWait
	for attempt := 1; ; attempt++ {
		err := c.send(ctx, method, path, data, in != nil, out)
		if err == nil || attempt == attempts || !transient(err) {
			return err
		}
		if werr := c.wait(ctx, wait); werr != nil {
			return errors.Wrapf(werr, "before sending %s %s again after: %s", method, path, err)
		}
		wait *= 2
	}
}

// send sends one request and decodes its answer.
func (c *Client) send(ctx context.Context, method, path string, data []byte, hasBody bool, out any) error {
	var body io.Reader
	if hasBody {
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return errors.Wrap(err, "http.NewRequestWithContext()")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.Wrap(err, "http.Client.Do()")
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return errors.Wrap(err, "io.ReadAll()")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		return errors.Wrap(&Error{Status: resp.StatusCode, Message: message(answer), Method: method, Path: path}, "github")
	}
	if out != nil && len(answer) > 0 {
		if err := json.Unmarshal(answer, out); err != nil {
			return errors.Wrap(err, "json.Unmarshal()")
		}
	}

	return nil
}

// transient reports whether the error is one a retry may cure: the request failed before
// an answer (http.Client.Do's error: a timeout, a reset connection), or GitHub answered
// 502, 503 or 504.
func transient(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusBadGateway || apiErr.Status == http.StatusServiceUnavailable || apiErr.Status == http.StatusGatewayTimeout
	}
	var urlErr *url.Error

	return errors.As(err, &urlErr)
}

// message is the API's message field, or the raw answer when there is none.
func message(data []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &m); err == nil && m.Message != "" {
		return m.Message
	}

	return strings.TrimSpace(string(data))
}

// Ref is a git reference and the object it points at.
type Ref struct {
	Ref    string `json:"ref"`
	Object Object `json:"object"`
}

// Object is what a ref or a tag points at: a commit, or for an annotated tag a tag
// object.
type Object struct {
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// Ref reads the reference named without its refs/ prefix (heads/master, tags/v1.0.0).
// An absent reference is NotFound.
func (c *Client) Ref(ctx context.Context, owner, repo, ref string) (*Ref, error) {
	r := &Ref{}
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/git/ref/"+ref, nil, r); err != nil {
		return nil, err
	}

	return r, nil
}

// TagCommit resolves the tag to the commit it names: a lightweight tag points at the
// commit, an annotated tag at a tag object that names it.
func (c *Client) TagCommit(ctx context.Context, owner, repo, tag string) (string, error) {
	ref, err := c.Ref(ctx, owner, repo, "tags/"+tag)
	if err != nil {
		return "", err
	}
	if ref.Object.Type != "tag" {
		return ref.Object.SHA, nil
	}
	var annotated struct {
		Object Object `json:"object"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/git/tags/"+ref.Object.SHA, nil, &annotated); err != nil {
		return "", err
	}

	return annotated.Object.SHA, nil
}

// CreateRef creates the full reference (refs/heads/...) at the commit.
func (c *Client) CreateRef(ctx context.Context, owner, repo, ref, sha string) error {
	in := struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	}{Ref: ref, SHA: sha}

	return c.do(ctx, http.MethodPost, "/repos/"+owner+"/"+repo+"/git/refs", in, nil)
}

// Comparison is how head relates to base: identical, ahead (head has commits base
// lacks), behind, or diverged; the commit the two share; and the files that differ
// between the merge base and head (the API lists up to 300).
type Comparison struct {
	Status          string         `json:"status"`
	MergeBaseCommit Object         `json:"merge_base_commit"`
	Files           []ComparedFile `json:"files"`
}

// ComparedFile is one file a comparison lists.
type ComparedFile struct {
	Filename string `json:"filename"`
}

// Compare compares head against base (a commit, branch or tag each).
func (c *Client) Compare(ctx context.Context, owner, repo, base, head string) (*Comparison, error) {
	cmp := &Comparison{}
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/compare/"+base+"..."+head, nil, cmp); err != nil {
		return nil, err
	}

	return cmp, nil
}

// Tag is one of the repository's tags with the commit it names (resolved through the
// tag object when the tag is annotated).
type Tag struct {
	Name   string `json:"name"`
	Commit Object `json:"commit"`
}

// Tags lists the repository's tags, every page of them.
func (c *Client) Tags(ctx context.Context, owner, repo string) ([]Tag, error) {
	var all []Tag
	for page := 1; ; page++ {
		var tags []Tag
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/tags?per_page=100&page=%d", owner, repo, page), nil, &tags); err != nil {
			return nil, err
		}
		all = append(all, tags...)
		if len(tags) < 100 {
			return all, nil
		}
	}
}

// Workflow is one of the repository's Actions workflows. GitHub lists a workflow once a
// push of at most twenty commits adds or changes its file on the default branch (the
// push event's commit list is capped there), or once it runs; a first push of more
// commits lists none of them until then.
type Workflow struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

// Workflows lists the repository's workflows, every page of them.
func (c *Client) Workflows(ctx context.Context, owner, repo string) ([]Workflow, error) {
	var all []Workflow
	for page := 1; ; page++ {
		var answer struct {
			Workflows []Workflow `json:"workflows"`
		}
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/actions/workflows?per_page=%d&page=%d", owner, repo, perPage, page), nil, &answer); err != nil {
			return nil, err
		}
		all = append(all, answer.Workflows...)
		if len(answer.Workflows) < perPage {
			return all, nil
		}
	}
}

// Branch is one of the repository's branches with the commit it points at.
type Branch struct {
	Name   string `json:"name"`
	Commit Object `json:"commit"`
}

// Empty reports whether the repository has no branch: nothing has been pushed to it yet.
// GitHub lists no branch for such a repository (and answers its commits with 409, Git
// Repository is empty), so the first branch alone is asked for.
func (c *Client) Empty(ctx context.Context, owner, repo string) (bool, error) {
	var branches []Branch
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/branches?per_page=1", nil, &branches); err != nil {
		return false, err
	}

	return len(branches) == 0, nil
}

// Commit is a commit's tree, which a new tree is built from.
type Commit struct {
	SHA  string `json:"sha"`
	Tree Object `json:"tree"`
}

// GetCommit reads the commit.
func (c *Client) GetCommit(ctx context.Context, owner, repo, sha string) (*Commit, error) {
	commit := &Commit{}
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/git/commits/"+sha, nil, commit); err != nil {
		return nil, err
	}

	return commit, nil
}

// Contents reads the file at path as it is at ref.
func (c *Client) Contents(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	var file struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/contents/"+path+"?ref="+ref, nil, &file); err != nil {
		return nil, err
	}
	if file.Encoding != "base64" {
		return nil, errors.Newf("contents of %s at %s: encoding %q, expected base64", path, ref, file.Encoding)
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil {
		return nil, errors.Wrap(err, "base64.StdEncoding.DecodeString()")
	}

	return data, nil
}

// TreeEntry is one file of a tree being built.
type TreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// CreateBlob stores the content and returns its SHA.
func (c *Client) CreateBlob(ctx context.Context, owner, repo string, content []byte) (string, error) {
	in := struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}{Content: base64.StdEncoding.EncodeToString(content), Encoding: "base64"}
	var out Object
	if err := c.do(ctx, http.MethodPost, "/repos/"+owner+"/"+repo+"/git/blobs", in, &out); err != nil {
		return "", err
	}

	return out.SHA, nil
}

// CreateTree makes a tree from the base tree with the entries changed, and returns its
// SHA.
func (c *Client) CreateTree(ctx context.Context, owner, repo, baseTree string, entries []TreeEntry) (string, error) {
	in := struct {
		BaseTree string      `json:"base_tree"`
		Tree     []TreeEntry `json:"tree"`
	}{BaseTree: baseTree, Tree: entries}
	var out Object
	if err := c.do(ctx, http.MethodPost, "/repos/"+owner+"/"+repo+"/git/trees", in, &out); err != nil {
		return "", err
	}

	return out.SHA, nil
}

// CreateCommit makes a commit of the tree on the parents and returns its SHA. The
// author is the token's account.
func (c *Client) CreateCommit(ctx context.Context, owner, repo, message, tree string, parents []string) (string, error) {
	in := struct {
		Message string   `json:"message"`
		Tree    string   `json:"tree"`
		Parents []string `json:"parents"`
	}{Message: message, Tree: tree, Parents: parents}
	var out Object
	if err := c.do(ctx, http.MethodPost, "/repos/"+owner+"/"+repo+"/git/commits", in, &out); err != nil {
		return "", err
	}

	return out.SHA, nil
}

// Base is the API host the client speaks to.
func (c *Client) Base() string {
	return c.base
}

// Comment is one comment on an issue or a pull request.
type Comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// IssueComments lists the latest comments on an issue or pull request, oldest first:
// the last page of a hundred, and the page before it when the last holds fewer than
// five, so the instruction a build ran on is found without walking a long thread.
func (c *Client) IssueComments(ctx context.Context, owner, repo string, number int) ([]Comment, error) {
	var issue struct {
		Comments int `json:"comments"`
	}
	issuePath := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	if err := c.do(ctx, http.MethodGet, issuePath, nil, &issue); err != nil {
		return nil, err
	}
	if issue.Comments == 0 {
		return []Comment{}, nil
	}
	last := (issue.Comments-1)/perPage + 1
	first := last
	if last > 1 && issue.Comments-(last-1)*perPage < commentWindow {
		first = last - 1
	}
	var all []Comment
	for page := first; page <= last; page++ {
		var comments []Comment
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s/comments?per_page=%d&page=%d", issuePath, perPage, page), nil, &comments); err != nil {
			return nil, err
		}
		all = append(all, comments...)
	}

	return all, nil
}
