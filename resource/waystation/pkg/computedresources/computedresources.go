// Package computedresources provides the waystation's computed resources: read-only
// resources whose rows come from application-written query logic instead of a table or
// a subquery. The generated handlers check permissions eagerly at decode time (there
// is no library execution underneath to defer to) and then call this package's List
// and Read functions.
package computedresources

// Client carries application dependencies into computed-resource query logic. The
// waystation's computed resources need only the resource client the generated handler
// already passes; the type exists to exercise the generated ComputedClient wiring.
type Client struct{}

// NewClient constructs a Client.
func NewClient() *Client {
	return &Client{}
}
