package generation

import (
	"context"
	"testing"
)

func TestApplicationName(t *testing.T) {
	testCases := []struct {
		name                    string
		opt                     ResourceOption
		expectedApplicationName string
		expectedReceiverName    string
	}{
		{
			name:                    "sets the application and receiver names",
			opt:                     ApplicationName("Server"),
			expectedApplicationName: "Server",
			expectedReceiverName:    "s",
		},
		{
			name:                    "uses the default application and receiver names",
			opt:                     nil,
			expectedApplicationName: "App",
			expectedReceiverName:    "a",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			opts := []ResourceOption{
				WithSpannerEmulatorVersion("1.5.41"),
			}
			if tc.opt != nil {
				opts = append(opts, tc.opt)
			}

			r, err := NewResourceGenerator(
				context.Background(),
				"testdata/resources.go",
				"file://generation/testdata/migrations",
				[]string{},
				opts...,
			)
			if err != nil {
				t.Fatalf("NewResourceGenerator() error = %v, want no error", err)
			}

			rg, ok := r.(*resourceGenerator)
			if !ok {
				t.Fatalf("expected a *resourceGenerator, got %T", r)
			}
			if rg.applicationName != tc.expectedApplicationName {
				t.Errorf("expected application name %q, got %q", tc.expectedApplicationName, rg.applicationName)
			}
			if rg.receiverName != tc.expectedReceiverName {
				t.Errorf("expected receiver name %q, got %q", tc.expectedReceiverName, rg.receiverName)
			}
		})
	}
}
