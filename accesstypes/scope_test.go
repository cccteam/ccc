package accesstypes

import (
	"testing"
)

func TestScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		scope      Scope
		wantGlobal bool
		wantDomain Domain
		wantOK     bool
		wantString string
	}{
		{
			name:       "global scope",
			scope:      GlobalScope(),
			wantGlobal: true,
			wantString: "global",
		},
		{
			name:       "tenant scope",
			scope:      DomainScope("station-alpha"),
			wantDomain: "station-alpha",
			wantOK:     true,
			wantString: "station-alpha",
		},
		{
			name:       "a tenant literally named global is a tenant scope",
			scope:      DomainScope("global"),
			wantDomain: "global",
			wantOK:     true,
			wantString: "global",
		},
		{
			name:       "a tenant named like the retired sentinel is a tenant scope",
			scope:      DomainScope("access:global"),
			wantDomain: "access:global",
			wantOK:     true,
			wantString: "access:global",
		},
		{
			name:       "zero scope is the zero domain's tenant scope",
			scope:      Scope{},
			wantOK:     true,
			wantString: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.scope.IsGlobal(); got != tt.wantGlobal {
				t.Errorf("IsGlobal() = %v, want %v", got, tt.wantGlobal)
			}
			domain, ok := tt.scope.Domain()
			if domain != tt.wantDomain || ok != tt.wantOK {
				t.Errorf("Domain() = (%q, %v), want (%q, %v)", domain, ok, tt.wantDomain, tt.wantOK)
			}
			if got := tt.scope.String(); got != tt.wantString {
				t.Errorf("String() = %q, want %q", got, tt.wantString)
			}
		})
	}
}

func TestScope_comparability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		a, b      Scope
		wantEqual bool
	}{
		{
			name:      "same tenant scopes are equal",
			a:         DomainScope("station-alpha"),
			b:         DomainScope("station-alpha"),
			wantEqual: true,
		},
		{
			name:      "global scopes are equal",
			a:         GlobalScope(),
			b:         GlobalScope(),
			wantEqual: true,
		},
		{
			name: "a tenant named global is not the global scope",
			a:    DomainScope("global"),
			b:    GlobalScope(),
		},
		{
			name: "different tenants differ",
			a:    DomainScope("station-alpha"),
			b:    DomainScope("station-beta"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.a == tt.b; got != tt.wantEqual {
				t.Errorf("(%v == %v) = %v, want %v", tt.a, tt.b, got, tt.wantEqual)
			}

			m := map[Scope]bool{tt.a: true}
			if got := m[tt.b]; got != tt.wantEqual {
				t.Errorf("map lookup via %v after storing %v = %v, want %v", tt.b, tt.a, got, tt.wantEqual)
			}
		})
	}
}
