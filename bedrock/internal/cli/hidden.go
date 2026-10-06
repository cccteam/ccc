// hidden.go reads a value at the terminal without echoing it.

package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/go-playground/errors/v5"
	"golang.org/x/term"
)

// readHidden prints the question on the writer and reads the answer from the terminal
// without echo, ending the line the person could not see.
func readHidden(w io.Writer, question string) ([]byte, error) {
	fmt.Fprint(w, question)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(w)
	if err != nil {
		return nil, errors.Wrap(err, "term.ReadPassword()")
	}

	return value, nil
}
