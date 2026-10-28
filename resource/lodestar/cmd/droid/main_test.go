package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-playground/errors/v5"
)

// TestParse pins the arguments the droid takes and the refusals it answers with.
//
// Demonstrates: outlet.programs.
func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    observation
		wantErr string
		// wantHelp marks -h: the usage is printed and the run ends with nothing to do.
		wantHelp bool
	}{
		{name: "a subsystem and a reading", args: []string{"reactor", "0.55"}, want: observation{subsystem: "reactor", reading: 0.55}},
		{name: "no arguments", wantErr: "two arguments are required"},
		{name: "a reading alone", args: []string{"0.55"}, wantErr: "two arguments are required"},
		{name: "an empty subsystem", args: []string{"", "0.55"}, wantErr: "the subsystem is required"},
		{name: "a reading that is not a number", args: []string{"reactor", "hot"}, wantErr: `"hot" is not a reading`},
		{name: "a flag is refused, since the droid takes none", args: []string{"-verbose", "reactor", "0.55"}, wantErr: errRefused.Error()},
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
				if !strings.Contains(stderr.String(), "droid <subsystem> <reading>") {
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

// TestNewReport pins the ingest payload: the ship, the subsystem, the reading, when it
// was taken, and the firmware's frame naming the firmware and the raw reading under the
// subsystem's key, which the service stores as it came.
//
// Demonstrates: outlet.programs.
func TestNewReport(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.November, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		settings    settings
		observation observation
		want        string
	}{
		{
			name:        "a reactor reading on the firmware the environment names",
			settings:    settings{Ship: "70000000-0000-4000-8000-000000000003", Firmware: "7.3"},
			observation: observation{subsystem: "reactor", reading: 0.55},
			want:        `{"shipId":"70000000-0000-4000-8000-000000000003","subsystem":"reactor","reading":0.55,"recordedAt":"2026-11-30T12:00:00Z","frame":{"fw":"7.3","reactor":{"raw":0.55,"takenAt":"2026-11-30T12:00:00Z"}}}`,
		},
		{
			name:        "a whole-number reading of another subsystem",
			settings:    settings{Ship: "70000000-0000-4000-8000-000000000001", Firmware: defaultFirmware},
			observation: observation{subsystem: "hull", reading: 3},
			want:        `{"shipId":"70000000-0000-4000-8000-000000000001","subsystem":"hull","reading":3,"recordedAt":"2026-11-30T12:00:00Z","frame":{"fw":"7.2","hull":{"raw":3,"takenAt":"2026-11-30T12:00:00Z"}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := newReport(&tt.settings, &tt.observation, at)
			if err != nil {
				t.Fatalf("newReport() error = %v", err)
			}
			got, err := json.Marshal(r)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("the report = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestIngestPath pins where the report goes: the ingest method under the outlet's prefix,
// in the sector.
func TestIngestPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		sector string
		want   string
	}{
		{name: "anvil", sector: "anvil", want: "/sectors/anvil/ingest-droid-reports"},
		{name: "cinder", sector: "cinder", want: "/sectors/cinder/ingest-droid-reports"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ingestPath(tt.sector); got != tt.want {
				t.Errorf("ingestPath(%q) = %q, want %q", tt.sector, got, tt.want)
			}
		})
	}
}
