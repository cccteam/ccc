package create

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/api/option"
)

// TestNewGoogleSplitsTheQuotaProjectFromStorage: the clients open with a quota project
// when the storage client is kept out of it, and Google's transport refuses the storage
// client a quota project beside the HTTP client it appends itself, which is why
// NewGoogleBilledTo splits the options. No client dials at construction; the credentials
// are left out so the test needs none.
func TestNewGoogleSplitsTheQuotaProjectFromStorage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	none := option.WithoutAuthentication()
	billed := []option.ClientOption{none, option.WithQuotaProject("imr-root-gbl-core-f2a4")}
	g, err := newGoogle(ctx, billed, []option.ClientOption{none})
	if err != nil {
		t.Fatalf("newGoogle() with the storage client kept out of the quota project = %v", err)
	}
	if err := g.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}
	if _, err := newGoogle(ctx, billed, billed); err == nil || !strings.Contains(err.Error(), "WithHTTPClient is incompatible with QuotaProject") {
		t.Errorf("newGoogle() with the quota project on the storage client too = %v, want Google's refusal, the reason for the split", err)
	}
}
