package accesstypes

import (
	"testing"
)

func TestDomainFromStringAndBack(t *testing.T) {
	t.Parallel()

	type args struct {
		domain string
	}
	tests := []struct {
		name string
		args args
		want Domain
	}{
		{
			name: "abc domain",
			args: args{
				domain: "domain:abc",
			},
			want: Domain("abc"),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := UnmarshalDomain(tt.args.domain)
			if got != tt.want {
				t.Errorf("DomainFromString() = %v, want %v", got, tt.want)
			}
			if gotString := got.Marshal(); gotString != tt.args.domain {
				t.Errorf("DomainFromString() = %v, want %v", gotString, tt.args.domain)
			}
		})
	}
}

func TestDomain_Marshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		d         Domain
		want      string
		wantPanic bool
	}{
		{
			name: "Success",
			d:    "MyDomain",
			want: domainPrefix + "MyDomain",
		},
		{
			name:      "Panic",
			d:         domainPrefix + "MyDomain",
			wantPanic: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				didPanic := recover()
				if tt.wantPanic != (didPanic != nil) {
					t.Errorf("User.Mashal() panic = %v, wantPanic %v", didPanic, tt.wantPanic)
				}
			}()
			if got := tt.d.Marshal(); got != tt.want {
				t.Errorf("Domain.Marshal() = %v, want %v", got, tt.want)
			}
		})
	}
}
