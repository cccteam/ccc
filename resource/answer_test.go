package resource

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
)

type choosingResult struct{ status int }

func (r *choosingResult) HTTPStatus() int { return r.status }

type plainResult struct{}

func TestResponseStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		result   any
		declared []int
		want     int
		wantErr  string
	}{
		{name: "the result's declared choice", result: &choosingResult{status: http.StatusConflict}, declared: []int{200, 409}, want: http.StatusConflict},
		{name: "a result that does not choose answers 200", result: &plainResult{}, declared: []int{200, 409}, want: http.StatusOK},
		{name: "an undeclared choice is an error naming the method and the code", result: &choosingResult{status: http.StatusTeapot}, declared: []int{200, 409}, wantErr: "CompleteMission answered with status 418"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ResponseStatus("CompleteMission", tt.result, tt.declared...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ResponseStatus() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ResponseStatus() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ResponseStatus() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantOK     bool
	}{
		{name: "an answer, wrapped, reports its status", err: errors.Wrap(&Answer{Status: http.StatusConflict}, "spanner.Client.ReadWriteTransaction()"), wantStatus: http.StatusConflict, wantOK: true},
		{name: "the dry-run sentinel is not an answer", err: ErrDryRun},
		{name: "nil is not an answer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, ok := Refused(tt.err)
			if ok != tt.wantOK || status != tt.wantStatus {
				t.Errorf("Refused() = (%d, %v), want (%d, %v)", status, ok, tt.wantStatus, tt.wantOK)
			}
		})
	}
}
