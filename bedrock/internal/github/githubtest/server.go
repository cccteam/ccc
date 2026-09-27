// Package githubtest is a GitHub API stand-in for tests: an in-memory organization with
// its installed apps and one or more repositories with rulesets, refs and tags, served
// over httptest so the real client speaks to it unchanged.
package githubtest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

// Repo is one repository's state.
type Repo struct {
	// Rulesets by ID.
	Rulesets map[int64]*github.Ruleset
	// Refs by full name (refs/heads/master, refs/tags/v0.1.0) to the object they point at.
	Refs map[string]github.Object
	// TagObjects by SHA: the commit each annotated tag object names.
	TagObjects map[string]string
	// Ancestry lists, per commit, the commits reachable from it (itself included), so a
	// comparison can be answered: base...head is identical when equal, ahead when base
	// is reachable from head, behind when the reverse, diverged otherwise.
	Ancestry map[string][]string
	// MergeBase by "a b" (either order) for a diverged pair.
	MergeBase map[string]string
	// Trees by commit SHA, and Files by "<tree>:<path>" to the blob's content; Blobs
	// by SHA. A commit made through the API gets a tree that copies its base tree's
	// files with the entries changed.
	Trees map[string]string
	Files map[string]string
	Blobs map[string]string
	// Comments by issue or pull request number, oldest first.
	Comments map[int][]github.Comment
}

// Server is the stand-in.
type Server struct {
	*httptest.Server
	mu            sync.Mutex
	Installations map[string][]github.Installation
	Repos         map[string]*Repo
	nextID        int64
	// Calls records every method and path served, in order; Messages the commit
	// messages of commits made through the API.
	Calls    []string
	Messages []string
}

var (
	rulesetsRE   = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/rulesets$`)
	rulesetRE    = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/rulesets/(\d+)$`)
	refRE        = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/git/ref/(.+)$`)
	refsRE       = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/git/refs$`)
	tagObjectRE  = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/git/tags/([^/]+)$`)
	compareRE    = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/compare/(.+)\.{3}(.+)$`)
	tagsRE       = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/tags$`)
	commitRE     = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/git/commits/([^/]+)$`)
	commitsRE    = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/git/commits$`)
	blobsRE      = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/git/blobs$`)
	treesRE      = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/git/trees$`)
	contentsRE   = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/contents/(.+)$`)
	installsRE   = regexp.MustCompile(`^/orgs/([^/]+)/installations$`)
	issueRE      = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/issues/(\d+)$`)
	commentsRE   = regexp.MustCompile(`^/repos/([^/]+)/([^/]+)/issues/(\d+)/comments$`)
	rulesetIDMin = int64(1000)
)

const (
	messageKey = "message"
	notFound   = "Not Found"
	typeCommit = "commit"
)

// New starts the stand-in; it stops when the test ends.
func New(t *testing.T) *Server {
	t.Helper()

	s := &Server{Installations: map[string][]github.Installation{}, Repos: map[string]*Repo{}, nextID: rulesetIDMin}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)

	return s
}

// Client is the real client pointed at the stand-in.
func (s *Server) Client() *github.Client {
	return github.New(s.URL, "test-token")
}

// AddRepo registers a repository with its state.
func (s *Server) AddRepo(owner, name string, repo *Repo) *Repo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if repo.Rulesets == nil {
		repo.Rulesets = map[int64]*github.Ruleset{}
	}
	if repo.Refs == nil {
		repo.Refs = map[string]github.Object{}
	}
	if repo.TagObjects == nil {
		repo.TagObjects = map[string]string{}
	}
	if repo.Ancestry == nil {
		repo.Ancestry = map[string][]string{}
	}
	if repo.MergeBase == nil {
		repo.MergeBase = map[string]string{}
	}
	if repo.Trees == nil {
		repo.Trees = map[string]string{}
	}
	if repo.Files == nil {
		repo.Files = map[string]string{}
	}
	if repo.Blobs == nil {
		repo.Blobs = map[string]string{}
	}
	if repo.Comments == nil {
		repo.Comments = map[int][]github.Comment{}
	}
	s.Repos[owner+"/"+name] = repo

	return repo
}

// route pairs a path pattern with what serves it; the submatches are the pattern's.
type route struct {
	re     *regexp.Regexp
	handle func(s *Server, w http.ResponseWriter, r *http.Request, m []string)
}

// routes are served in order; the first pattern to match wins.
var routes = []route{
	{installsRE, func(s *Server, w http.ResponseWriter, _ *http.Request, m []string) {
		reply(w, http.StatusOK, map[string]any{"installations": s.Installations[m[1]]})
	}},
	{rulesetsRE, func(s *Server, w http.ResponseWriter, r *http.Request, m []string) {
		s.rulesets(w, r, m[1]+"/"+m[2])
	}},
	{rulesetRE, func(s *Server, w http.ResponseWriter, r *http.Request, m []string) {
		id, _ := strconv.ParseInt(m[3], 10, 64)
		s.ruleset(w, r, m[1]+"/"+m[2], id)
	}},
	{refsRE, func(s *Server, w http.ResponseWriter, r *http.Request, m []string) {
		s.createRef(w, r, m[1]+"/"+m[2])
	}},
	{refRE, func(s *Server, w http.ResponseWriter, _ *http.Request, m []string) {
		s.ref(w, m[1]+"/"+m[2], "refs/"+m[3])
	}},
	{tagObjectRE, func(s *Server, w http.ResponseWriter, _ *http.Request, m []string) {
		s.tagObject(w, m[1]+"/"+m[2], m[3])
	}},
	{compareRE, func(s *Server, w http.ResponseWriter, _ *http.Request, m []string) {
		s.compare(w, m[1]+"/"+m[2], m[3], m[4])
	}},
	{tagsRE, func(s *Server, w http.ResponseWriter, _ *http.Request, m []string) {
		s.tags(w, m[1]+"/"+m[2])
	}},
	{commitRE, func(s *Server, w http.ResponseWriter, _ *http.Request, m []string) {
		s.commit(w, m[1]+"/"+m[2], m[3])
	}},
	{commitsRE, func(s *Server, w http.ResponseWriter, r *http.Request, m []string) {
		s.createCommit(w, r, m[1]+"/"+m[2])
	}},
	{blobsRE, func(s *Server, w http.ResponseWriter, r *http.Request, m []string) {
		s.createBlob(w, r, m[1]+"/"+m[2])
	}},
	{treesRE, func(s *Server, w http.ResponseWriter, r *http.Request, m []string) {
		s.createTree(w, r, m[1]+"/"+m[2])
	}},
	{contentsRE, func(s *Server, w http.ResponseWriter, r *http.Request, m []string) {
		s.contents(w, r, m[1]+"/"+m[2], m[3])
	}},
	{issueRE, func(s *Server, w http.ResponseWriter, _ *http.Request, m []string) {
		number, _ := strconv.Atoi(m[3])
		s.issue(w, m[1]+"/"+m[2], number)
	}},
	{commentsRE, func(s *Server, w http.ResponseWriter, r *http.Request, m []string) {
		number, _ := strconv.Atoi(m[3])
		s.comments(w, r, m[1]+"/"+m[2], number)
	}},
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := r.URL.Path
	s.Calls = append(s.Calls, r.Method+" "+path)
	if r.Header.Get("Authorization") != "Bearer test-token" {
		reply(w, http.StatusUnauthorized, map[string]string{messageKey: "Bad credentials"})

		return
	}
	for _, route := range routes {
		if m := route.re.FindStringSubmatch(path); m != nil {
			route.handle(s, w, r, m)

			return
		}
	}
	reply(w, http.StatusNotFound, map[string]string{messageKey: notFound})
}

func (s *Server) repo(w http.ResponseWriter, key string) (*Repo, bool) {
	repo, ok := s.Repos[key]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{messageKey: notFound})
	}

	return repo, ok
}

func (s *Server) rulesets(w http.ResponseWriter, r *http.Request, key string) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		list := make([]github.Ruleset, 0, len(repo.Rulesets))
		for _, rs := range repo.Rulesets {
			list = append(list, github.Ruleset{ID: rs.ID, Name: rs.Name, Target: rs.Target, Enforcement: rs.Enforcement})
		}
		reply(w, http.StatusOK, list)
	case http.MethodPost:
		rs := &github.Ruleset{}
		if err := json.NewDecoder(r.Body).Decode(rs); err != nil {
			reply(w, http.StatusUnprocessableEntity, map[string]string{messageKey: err.Error()})

			return
		}
		for _, existing := range repo.Rulesets {
			if existing.Name == rs.Name {
				reply(w, http.StatusUnprocessableEntity, map[string]string{messageKey: "Name has already been taken"})

				return
			}
		}
		rs.ID = s.nextID
		s.nextID++
		repo.Rulesets[rs.ID] = rs
		reply(w, http.StatusCreated, rs)
	default:
		reply(w, http.StatusMethodNotAllowed, map[string]string{messageKey: r.Method})
	}
}

func (s *Server) ruleset(w http.ResponseWriter, r *http.Request, key string, id int64) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	rs, ok := repo.Rulesets[id]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{messageKey: notFound})

		return
	}
	switch r.Method {
	case http.MethodGet:
		reply(w, http.StatusOK, rs)
	case http.MethodPut:
		updated := &github.Ruleset{}
		if err := json.NewDecoder(r.Body).Decode(updated); err != nil {
			reply(w, http.StatusUnprocessableEntity, map[string]string{messageKey: err.Error()})

			return
		}
		updated.ID = id
		repo.Rulesets[id] = updated
		reply(w, http.StatusOK, updated)
	default:
		reply(w, http.StatusMethodNotAllowed, map[string]string{messageKey: r.Method})
	}
}

func (s *Server) ref(w http.ResponseWriter, key, ref string) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	obj, ok := repo.Refs[ref]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{messageKey: notFound})

		return
	}
	reply(w, http.StatusOK, github.Ref{Ref: ref, Object: obj})
}

func (s *Server) createRef(w http.ResponseWriter, r *http.Request, key string) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	var in struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		reply(w, http.StatusUnprocessableEntity, map[string]string{messageKey: err.Error()})

		return
	}
	if _, exists := repo.Refs[in.Ref]; exists {
		reply(w, http.StatusUnprocessableEntity, map[string]string{messageKey: "Reference already exists"})

		return
	}
	repo.Refs[in.Ref] = github.Object{Type: typeCommit, SHA: in.SHA}
	reply(w, http.StatusCreated, github.Ref{Ref: in.Ref, Object: repo.Refs[in.Ref]})
}

func (s *Server) tagObject(w http.ResponseWriter, key, sha string) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	commit, ok := repo.TagObjects[sha]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{messageKey: notFound})

		return
	}
	reply(w, http.StatusOK, map[string]any{"sha": sha, "object": github.Object{Type: typeCommit, SHA: commit}})
}

// resolve turns a branch or tag name into a commit, or returns the name when it is one.
func (repo *Repo) resolve(name string) string {
	for _, ref := range []string{"refs/heads/" + name, "refs/tags/" + name} {
		if obj, ok := repo.Refs[ref]; ok {
			if obj.Type == "tag" {
				return repo.TagObjects[obj.SHA]
			}

			return obj.SHA
		}
	}

	return name
}

func (s *Server) compare(w http.ResponseWriter, key, base, head string) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	baseSHA, headSHA := repo.resolve(base), repo.resolve(head)
	var status, mergeBase string
	switch {
	case baseSHA == headSHA:
		status, mergeBase = "identical", baseSHA
	case reachable(repo.Ancestry[headSHA], baseSHA):
		status, mergeBase = "ahead", baseSHA
	case reachable(repo.Ancestry[baseSHA], headSHA):
		status, mergeBase = "behind", headSHA
	default:
		status = "diverged"
		mergeBase = repo.MergeBase[baseSHA+" "+headSHA]
		if mergeBase == "" {
			mergeBase = repo.MergeBase[headSHA+" "+baseSHA]
		}
	}
	reply(w, http.StatusOK, github.Comparison{Status: status, MergeBaseCommit: github.Object{Type: typeCommit, SHA: mergeBase}})
}

// tags lists the tags with their commits, annotated ones resolved.
func (s *Server) tags(w http.ResponseWriter, key string) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	var tags []github.Tag
	for ref, obj := range repo.Refs {
		if !strings.HasPrefix(ref, "refs/tags/") {
			continue
		}
		sha := obj.SHA
		if obj.Type == "tag" {
			sha = repo.TagObjects[sha]
		}
		tags = append(tags, github.Tag{Name: strings.TrimPrefix(ref, "refs/tags/"), Commit: github.Object{Type: typeCommit, SHA: sha}})
	}
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Name < tags[j].Name
	})
	reply(w, http.StatusOK, tags)
}

func (s *Server) commit(w http.ResponseWriter, key, sha string) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	tree, ok := repo.Trees[sha]
	if !ok {
		reply(w, http.StatusNotFound, map[string]string{messageKey: notFound})

		return
	}
	reply(w, http.StatusOK, github.Commit{SHA: sha, Tree: github.Object{Type: "tree", SHA: tree}})
}

func (s *Server) contents(w http.ResponseWriter, r *http.Request, key, path string) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	ref := r.URL.Query().Get("ref")
	tree, ok := repo.Trees[repo.resolve(ref)]
	content, found := repo.Files[tree+":"+path]
	if !ok || !found {
		reply(w, http.StatusNotFound, map[string]string{messageKey: notFound})

		return
	}
	reply(w, http.StatusOK, map[string]string{"content": base64.StdEncoding.EncodeToString([]byte(content)), "encoding": "base64"})
}

func (s *Server) createBlob(w http.ResponseWriter, r *http.Request, key string) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	var in struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Encoding != "base64" {
		reply(w, http.StatusUnprocessableEntity, map[string]string{messageKey: "content must be base64"})

		return
	}
	data, err := base64.StdEncoding.DecodeString(in.Content)
	if err != nil {
		reply(w, http.StatusUnprocessableEntity, map[string]string{messageKey: err.Error()})

		return
	}
	sha := s.newSHA("blob")
	repo.Blobs[sha] = string(data)
	reply(w, http.StatusCreated, github.Object{Type: "blob", SHA: sha})
}

func (s *Server) createTree(w http.ResponseWriter, r *http.Request, key string) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	var in struct {
		BaseTree string             `json:"base_tree"`
		Tree     []github.TreeEntry `json:"tree"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		reply(w, http.StatusUnprocessableEntity, map[string]string{messageKey: err.Error()})

		return
	}
	sha := s.newSHA("tree")
	for k, v := range repo.Files {
		if strings.HasPrefix(k, in.BaseTree+":") {
			repo.Files[sha+":"+strings.TrimPrefix(k, in.BaseTree+":")] = v
		}
	}
	for _, entry := range in.Tree {
		content, ok := repo.Blobs[entry.SHA]
		if !ok {
			reply(w, http.StatusUnprocessableEntity, map[string]string{messageKey: "no blob " + entry.SHA})

			return
		}
		repo.Files[sha+":"+entry.Path] = content
	}
	reply(w, http.StatusCreated, github.Object{Type: "tree", SHA: sha})
}

func (s *Server) createCommit(w http.ResponseWriter, r *http.Request, key string) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	var in struct {
		Message string   `json:"message"`
		Tree    string   `json:"tree"`
		Parents []string `json:"parents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		reply(w, http.StatusUnprocessableEntity, map[string]string{messageKey: err.Error()})

		return
	}
	sha := s.newSHA(typeCommit)
	repo.Trees[sha] = in.Tree
	ancestry := []string{sha}
	for _, parent := range in.Parents {
		ancestry = append(ancestry, repo.Ancestry[parent]...)
	}
	repo.Ancestry[sha] = ancestry
	s.Messages = append(s.Messages, in.Message)
	reply(w, http.StatusCreated, github.Object{Type: typeCommit, SHA: sha})
}

// newSHA is a made-up object name, distinct per call.
// issue answers the issue with its comment count, which the client pages by.
func (s *Server) issue(w http.ResponseWriter, key string, number int) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	reply(w, http.StatusOK, map[string]any{"number": number, "comments": len(repo.Comments[number])})
}

// comments answers one page of the issue's comments, as per_page and page ask.
func (s *Server) comments(w http.ResponseWriter, r *http.Request, key string, number int) {
	repo, ok := s.repo(w, key)
	if !ok {
		return
	}
	all := repo.Comments[number]
	size, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if size <= 0 {
		size = 30
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	start := min((page-1)*size, len(all))
	end := min(start+size, len(all))
	comments := make([]github.Comment, 0, end-start)
	comments = append(comments, all[start:end]...)
	reply(w, http.StatusOK, comments)
}

func (s *Server) newSHA(kind string) string {
	s.nextID++

	return kind + strconv.FormatInt(s.nextID, 10)
}

func reachable(ancestors []string, sha string) bool {
	for _, a := range ancestors {
		if a == sha {
			return true
		}
	}

	return false
}

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The client reads a truncated answer; the test then fails on it.
		_, _ = w.Write([]byte(strings.TrimSpace(err.Error())))
	}
}
