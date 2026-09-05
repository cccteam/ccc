package genlang

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestArg_ParseInvocations(t *testing.T) {
	t.Parallel()

	bindingSpec := &ArgSpec{Positional: 1, Keys: []string{"via"}}
	subjectSpec := &ArgSpec{Positional: 1, Keys: []string{"value"}, Required: []string{"value"}}
	transitionSpec := &ArgSpec{Positional: 1, Keys: []string{"from", "to"}, Required: []string{"from", "to"}, Multi: []string{"from"}}

	tests := []struct {
		name        string
		arg         Arg
		spec        *ArgSpec
		want        []NamedArgs
		wantContain string
	}{
		{
			name: "positional only",
			arg:  Arg("crew"),
			spec: bindingSpec,
			want: []NamedArgs{{Positional: []string{"crew"}, named: map[string][]string{}}},
		},
		{
			name: "positional with named",
			arg:  Arg("shipClass, via: Class"),
			spec: bindingSpec,
			want: []NamedArgs{{Positional: []string{"shipClass"}, named: map[string][]string{"via": {"Class"}}}},
		},
		{
			name: "dotted multi-hop value",
			arg:  Arg("sector, via: StationId.Sector"),
			spec: bindingSpec,
			want: []NamedArgs{{Positional: []string{"sector"}, named: map[string][]string{"via": {"StationId.Sector"}}}},
		},
		{
			name: "repeated keyword yields one parse per invocation",
			arg:  Arg("crews, value: CrewId\x00teams, value: TeamId"),
			spec: subjectSpec,
			want: []NamedArgs{
				{Positional: []string{"crews"}, named: map[string][]string{"value": {"CrewId"}}},
				{Positional: []string{"teams"}, named: map[string][]string{"value": {"TeamId"}}},
			},
		},
		{
			name: "whitespace is free",
			arg:  Arg("  crews ,   value:   CrewId  "),
			spec: subjectSpec,
			want: []NamedArgs{{Positional: []string{"crews"}, named: map[string][]string{"value": {"CrewId"}}}},
		},
		{
			name: "multi-valued key collects bare continuations",
			arg:  Arg("WorkOrder, from: draft, scheduled, to: canceled"),
			spec: transitionSpec,
			want: []NamedArgs{{Positional: []string{"WorkOrder"}, named: map[string][]string{"from": {"draft", "scheduled"}, "to": {"canceled"}}}},
		},
		{
			name: "multi-valued key with a single value",
			arg:  Arg("WorkOrder, from: draft, to: scheduled"),
			spec: transitionSpec,
			want: []NamedArgs{{Positional: []string{"WorkOrder"}, named: map[string][]string{"from": {"draft"}, "to": {"scheduled"}}}},
		},
		{
			name:        "bare continuation after a single-valued key",
			arg:         Arg("WorkOrder, from: draft, to: scheduled, extra"),
			spec:        transitionSpec,
			wantContain: "positional argument",
		},
		{
			name:        "missing required named argument",
			arg:         Arg("crews"),
			spec:        subjectSpec,
			wantContain: `missing required argument "value"`,
		},
		{
			name:        "unknown named argument",
			arg:         Arg("crew, path: Class"),
			spec:        bindingSpec,
			wantContain: `unknown argument "path"`,
		},
		{
			name:        "duplicate named argument",
			arg:         Arg("crew, via: A, via: B"),
			spec:        bindingSpec,
			wantContain: `given twice`,
		},
		{
			name:        "positional after named",
			arg:         Arg("via: Class, crew"),
			spec:        bindingSpec,
			wantContain: "positional argument",
		},
		{
			name:        "too many positional values",
			arg:         Arg("crew, extra"),
			spec:        bindingSpec,
			wantContain: "expected 1 positional argument(s), found 2",
		},
		{
			name:        "no positional value",
			arg:         Arg("via: Class"),
			spec:        bindingSpec,
			wantContain: "expected 1 positional argument(s), found 0",
		},
		{
			name:        "named argument with empty value",
			arg:         Arg("crew, via:"),
			spec:        bindingSpec,
			wantContain: "no value",
		},
		{
			name:        "empty argument",
			arg:         Arg("crew,,via: Class"),
			spec:        bindingSpec,
			wantContain: "empty argument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.arg.ParseInvocations(tt.spec)
			if tt.wantContain != "" {
				if err == nil {
					t.Fatalf("ParseInvocations(%q) expected an error containing %q, got nil", tt.arg, tt.wantContain)
				}
				if !strings.Contains(err.Error(), tt.wantContain) {
					t.Errorf("ParseInvocations(%q) error = %q, want containing %q", tt.arg, err, tt.wantContain)
				}

				return
			}
			if err != nil {
				t.Fatalf("ParseInvocations(%q) error = %v", tt.arg, err)
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(NamedArgs{})); diff != "" {
				t.Errorf("ParseInvocations(%q) mismatch (-want +got):\n%s", tt.arg, diff)
			}
		})
	}
}
