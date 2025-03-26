package resource

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCloneRequest(t *testing.T) {
	type args struct {
		requestBody string
	}
	tests := []struct {
		name     string
		args     args
		wantBody string
		wantErr  bool
	}{
		{
			name:     "Test 1",
			args:     args{requestBody: "test\nmultiline\nbody"},
			wantBody: "test\nmultiline\nbody",
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := io.NopCloser(strings.NewReader(tt.args.requestBody))
			r := &http.Request{
				Body: body,
			}

			for range 3 {
				r2, err := CloneRequest(r)
				if (err != nil) != tt.wantErr {
					t.Errorf("CloneRequest() error = %v, wantErr %v", err, tt.wantErr)

					return
				}

				got, err := io.ReadAll(r2.Body)
				if err != nil {
					t.Errorf("CloneRequest() error = %v", err)

					return
				}

				if string(got) != tt.wantBody {
					t.Errorf("CloneRequest() = %v, want %v", got, tt.wantBody)
				}
			}
		})
	}
}
