// Package github is the small GitHub REST client bedrock needs: the organization's
// installed apps, a repository's rulesets, and its refs, tags and comparisons. It
// speaks to one API host with one token and translates the API's refusals into errors
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
)

// Client speaks to one GitHub API host with one token.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// ClientFunc opens the client: the CLI wires Open, tests a client over a test server.
type ClientFunc func(ctx context.Context) (*Client, error)

// New is a client over the API at base, authenticated with the token.
func New(base, token string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

// Open finds a token and returns a client over the public API: GITHUB_TOKEN when set,
// else the token gh holds for its signed-in account. With neither it refuses.
func Open(ctx context.Context) (*Client, error) {
	if token := os.Getenv(tokenEnv); token != "" {
		return New(DefaultBase, token), nil
	}
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	token := strings.TrimSpace(string(out))
	if err != nil || token == "" {
		return nil, errors.Newf("no GitHub token: set %s, or sign in with gh (gh auth login) so that gh auth token prints one", tokenEnv)
	}

	return New(DefaultBase, token), nil
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

// do sends one request with the JSON body in (nil for none) and decodes the answer into
// out (nil to discard it). A status outside 2xx is an *Error.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return errors.Wrap(err, "json.Marshal()")
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return errors.Wrap(err, "http.NewRequestWithContext()")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.Wrap(err, "http.Client.Do()")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return errors.Wrap(err, "io.ReadAll()")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		return errors.Wrap(&Error{Status: resp.StatusCode, Message: message(data), Method: method, Path: path}, "github")
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return errors.Wrap(err, "json.Unmarshal()")
		}
	}

	return nil
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

// Installation is one GitHub App installed on an organization.
type Installation struct {
	ID      int64  `json:"id"`
	AppID   int64  `json:"app_id"`
	AppSlug string `json:"app_slug"`
}

// Installations lists the apps installed on the organization. The token must be an
// organization admin's.
func (c *Client) Installations(ctx context.Context, org string) ([]Installation, error) {
	var page struct {
		Installations []Installation `json:"installations"`
	}
	if err := c.do(ctx, http.MethodGet, "/orgs/"+org+"/installations?per_page=100", nil, &page); err != nil {
		return nil, err
	}

	return page.Installations, nil
}

// Ruleset is a repository ruleset as the API holds it. A listing carries the summary
// fields only; Ruleset by ID carries all of them.
type Ruleset struct {
	ID           int64         `json:"id,omitempty"`
	Name         string        `json:"name"`
	Target       string        `json:"target"`
	Enforcement  string        `json:"enforcement"`
	BypassActors []BypassActor `json:"bypass_actors"`
	Conditions   Conditions    `json:"conditions"`
	Rules        []Rule        `json:"rules"`
}

// BypassActor is who may bypass a ruleset: an Integration (a GitHub App, by its app
// ID), a RepositoryRole (5 is admin), an OrganizationAdmin (ID 1) or a Team.
type BypassActor struct {
	ActorID    int64  `json:"actor_id"`
	ActorType  string `json:"actor_type"`
	BypassMode string `json:"bypass_mode"`
}

// Conditions say which refs the ruleset covers.
type Conditions struct {
	RefName RefName `json:"ref_name"`
}

// RefName lists fnmatch patterns over full ref names (refs/heads/..., refs/tags/...).
type RefName struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

// Rule is one rule of a ruleset; Parameters is what the rule's type takes, when it
// takes any.
type Rule struct {
	Type       string         `json:"type"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// Rulesets lists the repository's rulesets (summaries).
func (c *Client) Rulesets(ctx context.Context, owner, repo string) ([]Ruleset, error) {
	var list []Ruleset
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/rulesets?per_page=100", nil, &list); err != nil {
		return nil, err
	}

	return list, nil
}

// Ruleset reads one ruleset in full.
func (c *Client) Ruleset(ctx context.Context, owner, repo string, id int64) (*Ruleset, error) {
	rs := &Ruleset{}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/rulesets/%d", owner, repo, id), nil, rs); err != nil {
		return nil, err
	}

	return rs, nil
}

// CreateRuleset creates the ruleset and returns it with its ID.
func (c *Client) CreateRuleset(ctx context.Context, owner, repo string, rs *Ruleset) (*Ruleset, error) {
	created := &Ruleset{}
	if err := c.do(ctx, http.MethodPost, "/repos/"+owner+"/"+repo+"/rulesets", rs, created); err != nil {
		return nil, err
	}

	return created, nil
}

// UpdateRuleset replaces the ruleset with rs.ID.
func (c *Client) UpdateRuleset(ctx context.Context, owner, repo string, rs *Ruleset) (*Ruleset, error) {
	updated := &Ruleset{}
	if err := c.do(ctx, http.MethodPut, fmt.Sprintf("/repos/%s/%s/rulesets/%d", owner, repo, rs.ID), rs, updated); err != nil {
		return nil, err
	}

	return updated, nil
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
// lacks), behind, or diverged; and the commit the two share.
type Comparison struct {
	Status          string `json:"status"`
	MergeBaseCommit Object `json:"merge_base_commit"`
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
