// Package live is the server side of live pages: a page that asked for a live list or
// a live row stays current without polling and without refetching while nothing
// changed, and a page mounted again inside a short window is served by the browser
// with no request. The server holds no push connection and keeps scaling to zero.
//
// A request opts in by carrying the subscribe header (X-Subscribe: <tab>). The
// generated list and read handlers register the subscription (who, which tab, which
// resource, which key or domain, until when) before they run the query, so a commit
// that lands during the query is not missed; a refused request registers nothing. A
// mutation publishes, after its commit and before its answer, one change document
// per subscriber into that user's change set, which the browser listens to and
// answers with a refetch carrying the change's timestamp as the version parameter
// (_v); a list or read response to such a request carries Cache-Control so a remount
// inside the window is the browser's own. Subscriptions expire unless the tab renews
// them through the renew route, where the server re-checks each against the user's
// grants; a tab unsubscribes when it leaves, and a logout unsubscribes everything and
// revokes the browser's identity.
//
// A row is named by its key as the read route spells it (resource.RowKey): a single
// key is its value's string form, a compound key is its parts joined with "/" in route
// order, the order the generated routes take them. The subscription record's key
// field, a renewal's key, and a change document's key all carry exactly that form, so
// the browser that addressed the row by its route and the publisher that saw the patch
// spell it identically.
//
// The service also carries the application's signals between its instances
// (Signaler, Subscriber): one signals document per application with a field per kind
// (features, tenants, policy), a signal of a kind writing that kind's field and waking
// every instance's subscriptions to the kind. The feature flags ride the features kind.
//
// The package holds the seams (SubscriptionRecord, ChangePublisher, Identity, Signaler,
// Subscriber, bundled as Service), the fan-out that every publisher implementation
// shares (Fanout), an in-memory Fake for tests, the handler-side glue the generated
// code calls (Subscribing, Refusing, Subscribe, SetCacheControl, Publish), and the
// three route handlers (RenewHandler, UnsubscribeHandler, TokenHandler). Every
// application wires a Service; the Firestore implementation is the firestore
// subpackage.
package live

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// The names the client library mirrors.
const (
	// SubscribeHeader carries the client-minted tab id on a request the page asked to
	// be live. Its value is 1 to 64 characters of [A-Za-z0-9_-].
	SubscribeHeader = "X-Subscribe"
	// VersionParam is the version query parameter, the browser cache's key: the change
	// document's timestamp as unix microseconds in decimal, or a seed minted at login.
	// The decoders accept and skip it (resource.VersionParam).
	VersionParam = resource.VersionParam
	// RenewRoute, UnsubscribeRoute and TokenRoute are the live routes under a session
	// outlet's API prefix.
	RenewRoute       = "live/renew"
	UnsubscribeRoute = "live/unsubscribe"
	TokenRoute       = "live/token"
	// LogAttribute is the request-log attribute every subscribing request carries.
	LogAttribute = "subscribe"
)

// The numbers.
const (
	// RenewInterval is how often a tab renews its subscriptions.
	RenewInterval = 120 * time.Second
	// SubscriptionTTL is how long a subscription lives after it was written or renewed:
	// about twice the renewal interval, so one missed renewal does not end it.
	SubscriptionTTL = 300 * time.Second
	// ChangeTTL is how long a change document stays in a user's change set.
	ChangeTTL = 600 * time.Second
	// CacheControl is the header value a list or read response carries when its request
	// carried the version parameter.
	CacheControl = "private, max-age=300"
	// BulkThreshold is the number of touched rows per resource in one request above
	// which the publisher stops looking subscribers up row by row and writes one
	// resource document per subscribed user instead.
	BulkThreshold = 100
	// PublishTimeout bounds a request's publish: past it the failure is logged and the
	// request answers.
	PublishTimeout = 2 * time.Second
)

// tabPattern is the shape of a tab id.
var tabPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidTab reports whether tab has the shape the header carries.
func ValidTab(tab string) bool {
	return tabPattern.MatchString(tab)
}

// Subscription is one live interest of one tab: a row (Key set, Domain empty) or a
// list (Key empty, Domain the tenant domain of a domain-scoped resource, empty for a
// global one). Key is the row's key as the read route spells it: a single key's string
// form, a compound key's parts joined with "/" in route order (resource.RowKey). Domain
// is never a permission concept in the record: it says which list a list subscription
// is of, and nothing else.
type Subscription struct {
	Principal string
	Tab       string
	Resource  accesstypes.Resource
	Key       string
	Domain    accesstypes.Domain
	Expiry    time.Time
}

// RowSubscription is the subscription a read registers: the row by resource and key.
func RowSubscription(res accesstypes.Resource, key string) *Subscription {
	return &Subscription{Resource: res, Key: key}
}

// ListSubscription is the subscription a list registers: the resource in the request's
// domain, which is empty for a global resource.
func ListSubscription(res accesstypes.Resource, domain accesstypes.Domain) *Subscription {
	return &Subscription{Resource: res, Domain: domain}
}

// IsRow reports whether the subscription names a row.
func (s *Subscription) IsRow() bool {
	return s.Key != ""
}

// Normalized returns the subscription as the record keeps it: a row subscription
// carries no domain.
func (s *Subscription) Normalized() Subscription {
	normalized := *s
	if normalized.IsRow() {
		normalized.Domain = ""
	}

	return normalized
}

// ID is the subscription's document id: the first 32 hex characters of
// sha256("principal|tab|resource|key|domain"), so the same interest of the same tab is
// one document however often it is written.
func (s *Subscription) ID() string {
	normalized := s.Normalized()
	sum := sha256.Sum256([]byte(normalized.Principal + "|" + normalized.Tab + "|" + string(normalized.Resource) + "|" + normalized.Key + "|" + string(normalized.Domain)))

	return hex.EncodeToString(sum[:])[:32]
}

// TokenPayload is what the token route answers: how the browser connects to the
// user's change set. Against the emulator Token is empty and Emulator names the host;
// in production Emulator is empty and Token is a Firebase custom token for UID.
type TokenPayload struct {
	UID      string `json:"uid"`
	Token    string `json:"token"`
	Project  string `json:"project"`
	Database string `json:"database"`
	APIKey   string `json:"apiKey"`
	Emulator string `json:"emulator"`
}

// ChangeKind is what a change document reports: one row, one list, or a whole resource.
type ChangeKind string

// The change kinds.
const (
	RowChange      ChangeKind = "row"
	ListChange     ChangeKind = "list"
	ResourceChange ChangeKind = "resource"
)

// ChangeDocument is one entry of a user's change set, as the publisher writes it and
// the browser reads it: a row document carries the key and whether the row was
// deleted, a list document the domain and no key, a resource document neither. The
// implementation adds the server timestamp (at) and the expiry (expires).
type ChangeDocument struct {
	Kind     ChangeKind
	Resource accesstypes.Resource
	Key      string
	Domain   accesstypes.Domain
	Deleted  bool
}

// ID is the document's id for the given unix second: row|<resource>|<key>|<second>,
// list|<resource>|<domain>|<second>, or resource|<resource>|<second>. Writes to one
// target within one second share an id and coalesce; a key's "/" (a compound key) is
// escaped, since a document id cannot carry one.
func (d ChangeDocument) ID(second int64) string {
	sec := strconv.FormatInt(second, 10)
	switch d.Kind {
	case RowChange:
		return string(RowChange) + "|" + string(d.Resource) + "|" + escapeKey(d.Key) + "|" + sec
	case ListChange:
		return string(ListChange) + "|" + string(d.Resource) + "|" + string(d.Domain) + "|" + sec
	default:
		return string(ResourceChange) + "|" + string(d.Resource) + "|" + sec
	}
}

// target is what two documents must share to coalesce: everything but the deleted flag.
func (d ChangeDocument) target() ChangeDocument {
	d.Deleted = false

	return d
}

// escapeKey makes a row key fit a document id segment.
func escapeKey(key string) string {
	escaped := make([]byte, 0, len(key))
	for i := range len(key) {
		if key[i] == '/' {
			escaped = append(escaped, "%2F"...)

			continue
		}
		escaped = append(escaped, key[i])
	}

	return string(escaped)
}

// Changes are the change documents one publish writes, per principal, in the order the
// fan-out produced them.
type Changes map[string][]ChangeDocument
