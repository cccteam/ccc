package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cccteam/ccc/securehash"
)

func TestHashCmd_Bcrypt_RoundTrip(t *testing.T) {
	t.Parallel()

	out, err := runRoot(t, []string{"hash", "--algo", "bcrypt", "--password", "hunter2"})
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	parsed := &securehash.Hash{}
	if err := parsed.UnmarshalText(bytes.TrimSpace(out)); err != nil {
		t.Fatalf("unmarshal hash output %q: %v", out, err)
	}
	if parsed.KeyType() != "Bcrypt" {
		t.Errorf("KeyType = %q, want Bcrypt", parsed.KeyType())
	}

	h := securehash.New(securehash.Bcrypt())
	if _, err := h.Compare(parsed, "hunter2"); err != nil {
		t.Errorf("Compare with correct password: %v", err)
	}
	if _, err := h.Compare(parsed, "WRONG"); err == nil {
		t.Error("Compare with wrong password: want error, got nil")
	}
}

func TestHashCmd_Argon2_JSON(t *testing.T) {
	t.Parallel()

	out, err := runRoot(t, []string{"hash", "--algo", "argon2", "--output", "json", "--password", "hunter2"})
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	var got struct {
		Algorithm string `json:"algorithm"`
		Hash      string `json:"hash"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal JSON %q: %v", out, err)
	}
	if got.Algorithm != "Argon2" {
		t.Errorf("algorithm = %q, want Argon2", got.Algorithm)
	}
	if !strings.HasPrefix(got.Hash, "1$") {
		t.Errorf("hash %q missing argon2 prefix", got.Hash)
	}
}

// runRoot executes the root command with the given args and returns stdout.
// stderr is discarded; --password is used so no stdin/TTY interaction is needed.
func runRoot(t *testing.T, args []string) ([]byte, error) {
	t.Helper()

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	err := root.Execute()

	return stdout.Bytes(), err
}
