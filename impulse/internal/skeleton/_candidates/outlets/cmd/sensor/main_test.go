package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/go-playground/errors/v5"
)

// TestParse pins the commands the sensor takes and the refusals it answers with.
func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    command
		wantErr string
		// wantHelp marks -h: the usage is printed and the run ends with nothing to do.
		wantHelp bool
	}{
		{name: "post a value", args: []string{"post", "21.5"}, want: command{word: commandPost, value: 21.5}},
		{name: "list", args: []string{"list"}, want: command{word: commandList}},
		{name: "no command", wantErr: "a command is required"},
		{name: "post without a value", args: []string{"post"}, wantErr: "post takes one argument"},
		{name: "post with a value that is not a number", args: []string{"post", "warm"}, wantErr: `"warm" is not a value`},
		{name: "list with an argument", args: []string{"list", "all"}, wantErr: "list takes no argument"},
		{name: "an unknown command", args: []string{"sweep"}, wantErr: `unknown command "sweep"`},
		{name: "a flag is refused, since the sensor takes none", args: []string{"-verbose", "list"}, wantErr: errRefused.Error()},
		{name: "help", args: []string{"-h"}, wantHelp: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			got, err := parse(tt.args, &stderr)
			switch {
			case tt.wantHelp:
				if !errors.Is(err, errHelp) {
					t.Fatalf("parse(%q) error = %v, want errHelp", tt.args, err)
				}
				if !strings.Contains(stderr.String(), "sensor post <value>") {
					t.Errorf("parse(%q) printed %q, want the usage", tt.args, stderr.String())
				}

				return
			case tt.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parse(%q) error = %v, want containing %q", tt.args, err, tt.wantErr)
				}

				return
			case err != nil:
				t.Fatalf("parse(%q) error = %v", tt.args, err)
			}
			if *got != tt.want {
				t.Errorf("parse(%q) = %+v, want %+v", tt.args, *got, tt.want)
			}
		})
	}
}

// TestPostBody pins the consolidated patch a post sends: one add under the tenant's
// readings, carrying the reading's create properties.
func TestPostBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tenant  string
		reading newReading
		want    string
	}{
		{
			name:    "one reading under the tenant",
			tenant:  "north",
			reading: newReading{Source: "pump-7", Value: 21.5, RecordedAt: time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)},
			want:    `[{"op":"add","path":"/tenants/north/readings","value":{"source":"pump-7","value":21.5,"recordedAt":"2026-10-27T12:00:00Z"}}]`,
		},
		{
			name:    "a whole-number value",
			tenant:  "south",
			reading: newReading{Source: "gate", Value: 3, RecordedAt: time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)},
			want:    `[{"op":"add","path":"/tenants/south/readings","value":{"source":"gate","value":3,"recordedAt":"2026-10-27T12:00:00Z"}}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := postBody(tt.tenant, tt.reading)
			if err != nil {
				t.Fatalf("postBody() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("postBody() = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestPrintReadings pins the table a list prints, and the line for none.
func TestPrintReadings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		readings []reading
		want     string
	}{
		{name: "no readings", want: "No readings\n"},
		{
			name: "two readings, newest first as listed",
			readings: []reading{
				{ID: "b", Source: "pump-7", Value: 21.5, RecordedAt: time.Date(2026, time.October, 27, 12, 0, 0, 0, time.UTC)},
				{ID: "a", Source: "pump-7", Value: 20, RecordedAt: time.Date(2026, time.October, 27, 11, 0, 0, 0, time.UTC)},
			},
			want: "RECORDED AT           SOURCE  VALUE  ID\n" +
				"2026-10-27T12:00:00Z  pump-7  21.5   b\n" +
				"2026-10-27T11:00:00Z  pump-7  20     a\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			printReadings(&out, tt.readings)
			if got := out.String(); got != tt.want {
				t.Errorf("printReadings() wrote:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}
