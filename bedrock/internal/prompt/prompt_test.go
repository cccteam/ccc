package prompt

import (
	"bytes"
	"strings"
	"testing"
)

func TestChoose(t *testing.T) {
	t.Parallel()

	envs := []Choice{{Value: "tst"}, {Value: "stg"}, {Value: "prd"}}
	versions := []Choice{{Value: "3", Note: "ENABLED"}, {Value: "2", Note: "DISABLED"}, {Value: "1", Note: "DESTROYED"}}
	tests := []struct {
		name    string
		input   string
		choices []Choice
		want    string
		wantErr string
		wantOut []string
	}{
		{name: "a number picks the choice at that position", input: "2\n", choices: envs, want: "stg", wantOut: []string{"Which environment?", "  1) tst", "  2) stg", "  3) prd", "> "}},
		{name: "the value itself is accepted", input: "prd\n", choices: envs, want: "prd"},
		{name: "spaces around the answer are ignored", input: "  tst  \n", choices: envs, want: "tst"},
		{name: "an answer without a line break is read", input: "stg", choices: envs, want: "stg"},
		{name: "a bad answer has the question asked again", input: "dev\n4\n1\n", choices: envs, want: "tst", wantOut: []string{`"dev" is not one of the choices.`, `"4" is not one of the choices.`}},
		{name: "three bad answers give up", input: "a\nb\nc\n", choices: envs, wantErr: "Which environment?: no choice made in 3 attempts"},
		{name: "input that ends before an answer is refused", input: "", choices: envs, wantErr: "no answer: the input ended"},
		{name: "values that are numbers are not numbered, and the number typed is the value", input: "1\n", choices: versions, want: "1", wantOut: []string{"  3  ENABLED", "  2  DISABLED", "  1  DESTROYED"}},
		{name: "a number outside a list of numbers is refused", input: "4\n3\n", choices: versions, want: "3", wantOut: []string{`"4" is not one of the choices.`}},
		{name: "no choices is refused", input: "1\n", choices: nil, wantErr: "nothing to choose from"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			got, err := New(strings.NewReader(tt.input), &out).Choose("Which environment?", tt.choices)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Choose() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Choose() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Choose() = %q, want %q", got, tt.want)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
		wantOut []string
	}{
		{name: "the line typed", input: "example.com\n", want: "example.com", wantOut: []string{"Which domain?", "> "}},
		{name: "spaces around it are dropped", input: "  example.com \n", want: "example.com"},
		{name: "an empty line has the question asked again", input: "\n\nexample.com\n", want: "example.com"},
		{name: "three empty lines give up", input: "\n\n\n", wantErr: "Which domain?: nothing typed in 3 attempts"},
		{name: "input that ends before an answer is refused", input: "", wantErr: "no answer: the input ended"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			got, err := New(strings.NewReader(tt.input), &out).Line("Which domain?")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Line() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Line() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Line() = %q, want %q", got, tt.want)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
		})
	}
}
