// requestlog.go derives the request log's exclusion from the surfaces the release file
// lists: the Cloud Logging filter that holds the entries Cloud Run and the load balancer
// write for the service to the request log words the code declares per surface.

package derive

import (
	"strconv"
	"strings"
)

// RequestLogExclusion is the request log's exclusion the stack renders for the
// application (logging.tf): one project-level Cloud Logging exclusion per environment,
// never in a pull-request stack, whose filter drops the entries the code's request log
// words say not to write.
//
// Cloud Run and the load balancer each write an entry for every request the service
// answers, whatever the application's own logger decides, so a surface the code logs
// on event alone would still cost an entry per quiet request at the edge. The
// exclusion's filter is the scope AND the Clause here. The scope is the service's own
// entries, Cloud Run's by the regional services' names and the load balancer's by the
// backend's, and never the project's: one project holds several applications, and
// another application's entries are not this stack's to drop. The stack writes the
// scope, since it names its resources; the clause is derived here.
type RequestLogExclusion struct {
	// Surfaces are the surfaces whose word excludes an entry, in prefix order: those
	// logged on event, sampled or never. A surface logged always, or declaring its
	// trace setting alone, has no clause and is not among them.
	Surfaces []Surface
	// Clause is the surfaces' clause of the filter, as the Logging query language reads
	// it: each surface's Clause, the several joined with OR inside one pair of
	// parentheses.
	Clause string
}

// NewRequestLogExclusion derives the exclusion from the surfaces, in their order: nil
// when no surface's word excludes an entry, and then the stack renders none, since
// every entry is written as it is today.
func NewRequestLogExclusion(surfaces []Surface) *RequestLogExclusion {
	var excluding []Surface
	var clauses []string
	for i := range surfaces {
		s := &surfaces[i]
		if !s.Excludes() {
			continue
		}
		excluding = append(excluding, *s)
		clauses = append(clauses, s.Clause())
	}
	if len(excluding) == 0 {
		return nil
	}

	return &RequestLogExclusion{Surfaces: excluding, Clause: "(" + strings.Join(clauses, " OR ") + ")"}
}

// Excludes reports whether the surface's word excludes an entry: on event, sampled and
// never do; always, and a surface declaring its trace setting alone, do not.
func (s *Surface) Excludes() bool {
	switch s.Log {
	case RequestLogOnEvent, RequestLogSampled, RequestLogNever:
		return true
	default:
		return false
	}
}

// Clause is the surface's clause of the exclusion's filter, by its word. Every clause
// matches the request's URL against the surface's prefix as a regular expression
// anchored after any host (^https://[^/]+<prefix>): the environment's hostnames and the
// next revision's all reach the same service, and the word is the path's, not the
// host's. On event drops the entry of a request that answered below 400, since the
// infrastructure cannot know whether a line attached and keeps every failure; sampled
// drops the same but the declared fraction of them, decided by the entry's insertId;
// never drops every entry under the prefix. Always has no clause, so the method is not
// called for it (Excludes).
func (s *Surface) Clause() string {
	url := `httpRequest.requestUrl =~ "^https://[^/]+` + PathRegex(s.Prefix) + `"`
	switch s.Log {
	case RequestLogOnEvent:
		return "(" + url + " AND httpRequest.status < 400)"
	case RequestLogSampled:
		return "(" + url + " AND httpRequest.status < 400 AND NOT sample(insertId, " + strconv.FormatFloat(s.Fraction, 'f', -1, 64) + "))"
	default:
		return "(" + url + ")"
	}
}

// Policy says the surface's request log word in prose, for a comment: logged always,
// logged on event, sampled at the fraction, or never logged; the trace setting alone
// when the surface declares no word.
func (s *Surface) Policy() string {
	switch s.Log {
	case RequestLogAlways:
		return "logged always"
	case RequestLogOnEvent:
		return "logged on event"
	case RequestLogSampled:
		return "sampled at " + strconv.FormatFloat(s.Fraction, 'f', -1, 64)
	case RequestLogNever:
		return "never logged"
	default:
		return "no request log word, its trace setting alone"
	}
}

// PathRegex is the route or prefix as the RE2 expression Cloud Armor matches the path
// against and Cloud Logging the request's URL: a parameter in braces matches one
// segment, a last star the subtree, and a dot matches itself through a class, so the
// expression carries no backslash to escape in CEL, in a Logging query or in HCL. A
// route's other characters (letters, digits, dashes, underscores, tildes, slashes)
// match themselves.
func PathRegex(route string) string {
	segments := strings.Split(route, "/")
	for i, seg := range segments {
		switch {
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}"):
			segments[i] = "[^/]+"
		case seg == "*":
			segments[i] = ".*"
		default:
			segments[i] = strings.ReplaceAll(seg, ".", "[.]")
		}
	}

	return strings.Join(segments, "/")
}
