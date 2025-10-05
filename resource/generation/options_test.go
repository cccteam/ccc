package generation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplicationName(t *testing.T) {
	t.Run("sets the application and receiver names", func(t *testing.T) {
		r, err := NewResourceGenerator(
			context.Background(),
			"testdata/resources.go",
			"file://generation/testdata/migrations",
			[]string{},
			ApplicationName("Server"),
			WithSpannerEmulatorVersion("1.5.4"),
		)
		require.NoError(t, err)

		rg := r.(*resourceGenerator)
		assert.Equal(t, "Server", rg.applicationName)
		assert.Equal(t, "s", rg.receiverName)
	})

	t.Run("uses the default application and receiver names", func(t *testing.T) {
		r, err := NewResourceGenerator(
			context.Background(),
			"testdata/resources.go",
			"file://generation/testdata/migrations",
			[]string{},
			WithSpannerEmulatorVersion("1.5.4"),
		)
		require.NoError(t, err)

		rg := r.(*resourceGenerator)
		assert.Equal(t, "App", rg.applicationName)
		assert.Equal(t, "a", rg.receiverName)
	})
}