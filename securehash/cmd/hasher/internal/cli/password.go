package cli

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"io"
	"os"

	"github.com/go-playground/errors/v5"
	"golang.org/x/term"
)

// passwordSource describes how to obtain a password.
type passwordSource struct {
	// flagValue is the literal value of --password, "" when unset.
	flagValue string
	// confirm prompts twice and ensures both entries match (TTY only).
	confirm bool

	// Inputs/outputs — wired to os.Stdin/Stderr in production, swapped in tests.
	in    io.Reader
	err   io.Writer
	stdin *os.File // for term.IsTerminal / term.ReadPassword; nil ⇒ never treat as TTY
}

func defaultPasswordSource() *passwordSource {
	return &passwordSource{in: os.Stdin, err: os.Stderr, stdin: os.Stdin}
}

// readPassword resolves a password from the configured source.
//
// Precedence:
//  1. flagValue (--password) — emits a stderr warning.
//  2. piped stdin — reads everything, strips a single trailing newline.
//  3. hidden TTY prompt — uses term.ReadPassword; confirms when ps.confirm.
func (ps *passwordSource) readPassword() ([]byte, error) {
	if ps.flagValue != "" {
		fmt.Fprintln(ps.err, "warning: --password is visible in shell history and the process list")

		return []byte(ps.flagValue), nil
	}

	if ps.stdin == nil || !term.IsTerminal(int(ps.stdin.Fd())) {
		b, err := io.ReadAll(ps.in)
		if err != nil {
			return nil, errors.Wrap(err, "read stdin")
		}
		b = bytes.TrimRight(b, "\r\n")
		if len(b) == 0 {
			return nil, errors.New("empty password on stdin")
		}

		return b, nil
	}

	fmt.Fprint(ps.err, "Password: ")
	pw, err := term.ReadPassword(int(ps.stdin.Fd()))
	fmt.Fprintln(ps.err)
	if err != nil {
		return nil, errors.Wrap(err, "read password")
	}
	if len(pw) == 0 {
		return nil, errors.New("empty password")
	}

	if !ps.confirm {
		return pw, nil
	}

	fmt.Fprint(ps.err, "Confirm: ")
	confirm, err := term.ReadPassword(int(ps.stdin.Fd()))
	fmt.Fprintln(ps.err)
	if err != nil {
		return nil, errors.Wrap(err, "read confirmation")
	}
	if subtle.ConstantTimeCompare(pw, confirm) != 1 {
		return nil, errors.New("passwords do not match")
	}

	return pw, nil
}
