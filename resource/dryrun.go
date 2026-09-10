package resource

import (
	"net/http"
	"strings"

	"github.com/go-playground/errors/v5"
)

// DryRunHeader asks a transaction-form RPC method to run its whole frame — decode,
// the entry check, the target's location and state checks, the body with every
// write it arms — and then roll the transaction back instead of committing. Every
// refusal answers exactly as the real call would; a call that would have succeeded
// answers 200 with no body, since a result could name rows that never came to be.
// A client-form method refuses the header: effects outside a transaction cannot be
// previewed.
const DryRunHeader = "X-Dry-Run"

// IsDryRun reports whether the request asks for a dry run.
func IsDryRun(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get(DryRunHeader), "true")
}

// ErrDryRun is the sentinel a generated frame returns from its transaction function
// on a dry run: the transaction rolls back and nothing retries. It never reaches
// the wire.
var ErrDryRun = errors.New("dry run: the transaction rolls back by design")

// DryRunRolledBack reports whether a transaction ended by the dry-run sentinel.
func DryRunRolledBack(err error) bool {
	return errors.Is(err, ErrDryRun)
}
