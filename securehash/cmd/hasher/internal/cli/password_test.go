package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadPassword_FromFlag(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	ps := &passwordSource{flagValue: "hunter2", err: &stderr}

	got, err := ps.readPassword()
	if err != nil {
		t.Fatalf("readPassword: %v", err)
	}
	if string(got) != "hunter2" {
		t.Errorf("password = %q, want %q", got, "hunter2")
	}
	if !strings.Contains(stderr.String(), "warning") {
		t.Errorf("expected warning on stderr, got %q", stderr.String())
	}
}

func TestReadPassword_FromStdinPipe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"trailing newline", "hunter2\n", "hunter2"},
		{"crlf", "hunter2\r\n", "hunter2"},
		{"no newline", "hunter2", "hunter2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ps := &passwordSource{in: strings.NewReader(tc.in), err: &bytes.Buffer{}}
			got, err := ps.readPassword()
			if err != nil {
				t.Fatalf("readPassword: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("password = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadPassword_EmptyStdin(t *testing.T) {
	t.Parallel()

	ps := &passwordSource{in: strings.NewReader(""), err: &bytes.Buffer{}}
	if _, err := ps.readPassword(); err == nil {
		t.Fatal("expected error on empty stdin, got nil")
	}
}
