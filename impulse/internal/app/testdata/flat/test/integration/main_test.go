package integration

import (
	"context"
	"testing"

	initiator "github.com/cccteam/db-initiator"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	c, err := initiator.NewSpannerContainer(ctx, "1.5.56")
	_, _ = c, err
	m.Run()
}
