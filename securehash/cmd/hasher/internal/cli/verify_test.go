package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyCmd_RoundTrip_Match(t *testing.T) {
	t.Parallel()

	hashOut, err := runRoot(t, []string{"hash", "--algo", "argon2", "--password", "hunter2"})
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	hashStr := strings.TrimSpace(string(hashOut))

	out, err := runRoot(t, []string{"verify", "--algo", "argon2", "--password", "hunter2", hashStr})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !strings.Contains(string(out), "match") || strings.Contains(string(out), "no match") {
		t.Errorf("verify stdout = %q, want 'match'", out)
	}
}

func TestVerifyCmd_RoundTrip_Mismatch(t *testing.T) {
	t.Parallel()

	hashOut, err := runRoot(t, []string{"hash", "--algo", "argon2", "--password", "hunter2"})
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	hashStr := strings.TrimSpace(string(hashOut))

	out, err := runRoot(t, []string{"verify", "--algo", "argon2", "--password", "WRONG", hashStr})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("verify err = %v, want *ExitError", err)
	}
	if exitErr.Code != exitMismatch {
		t.Errorf("exit code = %d, want %d", exitErr.Code, exitMismatch)
	}
	if !strings.Contains(string(out), "no match") {
		t.Errorf("verify stdout = %q, want 'no match'", out)
	}
}

func TestVerifyCmd_FromFile_JSON(t *testing.T) {
	t.Parallel()

	hashOut, err := runRoot(t, []string{"hash", "--algo", "argon2", "--password", "hunter2"})
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "h.txt")
	if err := os.WriteFile(path, hashOut, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runRoot(t, []string{"verify", "--algo", "argon2", "--output", "json", "--password", "hunter2", "--hash-file", path})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	var got verifyJSON
	if err := json.Unmarshal(bytes.TrimSpace(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if !got.Match {
		t.Errorf("match = false, want true (out=%q)", out)
	}
}

func TestVerifyCmd_BadHash(t *testing.T) {
	t.Parallel()

	_, err := runRoot(t, []string{"verify", "--password", "hunter2", "not-a-hash"})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want *ExitError", err)
	}
	if exitErr.Code != exitVerifyErr {
		t.Errorf("exit code = %d, want %d", exitErr.Code, exitVerifyErr)
	}
}
