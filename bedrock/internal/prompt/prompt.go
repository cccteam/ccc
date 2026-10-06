// Package prompt asks a person at the terminal for what the command line left out: the
// question is printed with the choices, one line is read back, and a line that answers
// nothing has the question asked again, a few times, before the command gives up.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
)

// attempts is how many times a question is asked before the command gives up.
const attempts = 3

// Choice is one answer that can be picked.
type Choice struct {
	// Value is the answer: what the command goes on with.
	Value string
	// Note is shown beside the value, such as a version's state; empty for none.
	Note string
}

// Prompter reads answers from one reader and writes questions to one writer.
type Prompter struct {
	in  *bufio.Reader
	out io.Writer
}

// New makes a Prompter over the reader and writer: the terminal's input and output.
func New(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{in: bufio.NewReader(in), out: out}
}

// Choose prints the question and the choices and returns the value picked. The line
// typed is a value, or the number of a choice in the list; the choices are numbered
// unless every value is a number itself, when the number typed is the value. A line that
// is neither has the question asked again, up to three times.
func (p *Prompter) Choose(question string, choices []Choice) (string, error) {
	if len(choices) == 0 {
		return "", errors.Newf("%s: nothing to choose from", question)
	}
	numbered := !allNumbers(choices)
	for attempt := 1; attempt <= attempts; attempt++ {
		fmt.Fprintln(p.out, question)
		for i, c := range choices {
			p.printChoice(i, c, numbered)
		}
		line, err := p.read()
		if err != nil {
			return "", err
		}
		if value, ok := pick(line, choices, numbered); ok {
			return value, nil
		}
		fmt.Fprintf(p.out, "%q is not one of the choices.\n", line)
	}

	return "", errors.Newf("%s: no choice made in %d attempts", question, attempts)
}

// printChoice prints one choice on its own line.
func (p *Prompter) printChoice(i int, c Choice, numbered bool) {
	if numbered {
		fmt.Fprintf(p.out, "  %d) %s", i+1, c.Value)
	} else {
		fmt.Fprintf(p.out, "  %s", c.Value)
	}
	if c.Note != "" {
		fmt.Fprintf(p.out, "  %s", c.Note)
	}
	fmt.Fprintln(p.out)
}

// pick is the value the line picks: the value it spells, else, in a numbered list, the
// value at the number it spells.
func pick(line string, choices []Choice, numbered bool) (string, bool) {
	for _, c := range choices {
		if line == c.Value {
			return c.Value, true
		}
	}
	if !numbered {
		return "", false
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(choices) {
		return "", false
	}

	return choices[n-1].Value, true
}

// allNumbers reports whether every choice's value is a number.
func allNumbers(choices []Choice) bool {
	for _, c := range choices {
		if _, err := strconv.Atoi(c.Value); err != nil {
			return false
		}
	}

	return true
}

// Line prints the question and returns the line typed, trimmed. An empty line has the
// question asked again, up to three times.
func (p *Prompter) Line(question string) (string, error) {
	for attempt := 1; attempt <= attempts; attempt++ {
		fmt.Fprintln(p.out, question)
		line, err := p.read()
		if err != nil {
			return "", err
		}
		if line != "" {
			return line, nil
		}
	}

	return "", errors.Newf("%s: nothing typed in %d attempts", question, attempts)
}

// read prints the answer marker and reads one line, trimmed. Input that ends without a
// line break still yields what was typed; input that ends with nothing is refused.
func (p *Prompter) read() (string, error) {
	fmt.Fprint(p.out, "> ")
	line, err := p.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", errors.Wrap(err, "bufio.Reader.ReadString()")
	}
	line = strings.TrimSpace(line)
	if err != nil && line == "" {
		return "", errors.New("no answer: the input ended")
	}

	return line, nil
}
