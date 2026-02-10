package tracer

import (
	"testing"

	"github.com/go-playground/errors/v5"
)

func Test_traceResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name: "Do not error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := traceResource("test")
			if (err != nil) != tt.wantErr {
				t.Errorf("traceResource() error = %v, wantErr %v", errors.Cause(err), tt.wantErr)
				return
			}
		})
	}
}
