package accesstypes

import (
	"reflect"
	"testing"
)

func TestPolicyScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		scope      PolicyScope
		wantGlobal bool
		wantEvery  bool
		wantDomain Domain
		wantOK     bool
		wantString string
	}{
		{
			name:       "the global partition",
			scope:      GlobalPolicyScope(),
			wantGlobal: true,
			wantString: "global",
		},
		{
			name:       "one tenant domain",
			scope:      DomainPolicyScope("station-alpha"),
			wantDomain: "station-alpha",
			wantOK:     true,
			wantString: "station-alpha",
		},
		{
			name:       "every tenant domain",
			scope:      EveryDomainPolicyScope(),
			wantEvery:  true,
			wantString: "every domain",
		},
		{
			name:       "a tenant literally named global is one domain",
			scope:      DomainPolicyScope("global"),
			wantDomain: "global",
			wantOK:     true,
			wantString: "global",
		},
		{
			name:       "a tenant literally named every domain is one domain",
			scope:      DomainPolicyScope("every domain"),
			wantDomain: "every domain",
			wantOK:     true,
			wantString: "every domain",
		},
		{
			name:       "the zero value is the zero domain's partition",
			scope:      PolicyScope{},
			wantOK:     true,
			wantString: "",
		},
		{
			name:       "the global scope converts to the global partition",
			scope:      GlobalScope().PolicyScope(),
			wantGlobal: true,
			wantString: "global",
		},
		{
			name:       "a tenant scope converts to its one domain",
			scope:      DomainScope("station-beta").PolicyScope(),
			wantDomain: "station-beta",
			wantOK:     true,
			wantString: "station-beta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.scope.IsGlobal(); got != tt.wantGlobal {
				t.Errorf("IsGlobal() = %v, want %v", got, tt.wantGlobal)
			}
			if got := tt.scope.IsEveryDomain(); got != tt.wantEvery {
				t.Errorf("IsEveryDomain() = %v, want %v", got, tt.wantEvery)
			}
			domain, ok := tt.scope.Domain()
			if domain != tt.wantDomain || ok != tt.wantOK {
				t.Errorf("Domain() = (%q, %v), want (%q, %v)", domain, ok, tt.wantDomain, tt.wantOK)
			}
			if got := tt.scope.String(); got != tt.wantString {
				t.Errorf("String() = %q, want %q", got, tt.wantString)
			}
			if got := tt.scope.Axis(); got != "" {
				t.Errorf("Axis() = %q, want the default axis %q", got, "")
			}
		})
	}
}

func TestPolicyScope_Covers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		held  PolicyScope
		scope Scope
		want  bool
	}{
		{name: "global covers the global scope", held: GlobalPolicyScope(), scope: GlobalScope(), want: true},
		{name: "global does not cover a tenant", held: GlobalPolicyScope(), scope: DomainScope("anvil")},
		{name: "one domain covers its own scope", held: DomainPolicyScope("anvil"), scope: DomainScope("anvil"), want: true},
		{name: "one domain does not cover another", held: DomainPolicyScope("anvil"), scope: DomainScope("bastion")},
		{name: "one domain does not cover the global scope", held: DomainPolicyScope("anvil"), scope: GlobalScope()},
		{name: "a domain named global does not cover the global scope", held: DomainPolicyScope("global"), scope: GlobalScope()},
		{name: "every domain covers a tenant the store never saw", held: EveryDomainPolicyScope(), scope: DomainScope("cinder"), want: true},
		{name: "every domain does not cover the global scope", held: EveryDomainPolicyScope(), scope: GlobalScope()},
		{name: "the zero value covers only the zero domain", held: PolicyScope{}, scope: DomainScope(""), want: true},
		{name: "the zero value does not cover a named domain", held: PolicyScope{}, scope: DomainScope("anvil")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.held.Covers(tt.scope); got != tt.want {
				t.Errorf("%v.Covers(%v) = %v, want %v", tt.held, tt.scope, got, tt.want)
			}
		})
	}
}

func TestPolicyScope_comparability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		a, b      PolicyScope
		wantEqual bool
	}{
		{name: "the same domain twice is equal", a: DomainPolicyScope("anvil"), b: DomainPolicyScope("anvil"), wantEqual: true},
		{name: "global values are equal", a: GlobalPolicyScope(), b: GlobalPolicyScope(), wantEqual: true},
		{name: "every-domain values are equal", a: EveryDomainPolicyScope(), b: EveryDomainPolicyScope(), wantEqual: true},
		{name: "a converted tenant scope equals the domain constructor", a: DomainScope("anvil").PolicyScope(), b: DomainPolicyScope("anvil"), wantEqual: true},
		{name: "a converted global scope equals the global constructor", a: GlobalScope().PolicyScope(), b: GlobalPolicyScope(), wantEqual: true},
		{name: "a domain named global is not the global partition", a: DomainPolicyScope("global"), b: GlobalPolicyScope()},
		{name: "a domain named every domain is not every domain", a: DomainPolicyScope("every domain"), b: EveryDomainPolicyScope()},
		{name: "every domain is not the global partition", a: EveryDomainPolicyScope(), b: GlobalPolicyScope()},
		{name: "different domains differ", a: DomainPolicyScope("anvil"), b: DomainPolicyScope("bastion")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.a == tt.b; got != tt.wantEqual {
				t.Errorf("(%v == %v) = %v, want %v", tt.a, tt.b, got, tt.wantEqual)
			}
			m := map[PolicyScope]bool{tt.a: true}
			if got := m[tt.b]; got != tt.wantEqual {
				t.Errorf("map lookup via %v after storing %v = %v, want %v", tt.b, tt.a, got, tt.wantEqual)
			}
		})
	}
}

// TestPolicyScope_convertsOneWay pins the rule the two types rest on: a Scope
// converts to the place its policy is held, and no method of PolicyScope
// yields a Scope, so a value of the every-domain kind can never reach a
// permission check through this package.
func TestPolicyScope_convertsOneWay(t *testing.T) {
	t.Parallel()

	scopeType := reflect.TypeFor[Scope]()
	policyType := reflect.TypeFor[PolicyScope]()
	if _, ok := scopeType.MethodByName("PolicyScope"); !ok {
		t.Error("Scope has no PolicyScope method: a partition must convert to the place its policy is held")
	}
	for i := range policyType.NumMethod() {
		m := policyType.Method(i)
		for j := range m.Type.NumOut() {
			if m.Type.Out(j) == scopeType {
				t.Errorf("PolicyScope.%s returns a Scope: nothing may convert a place policy is held into a partition a request is in", m.Name)
			}
		}
	}
	if policyType.ConvertibleTo(scopeType) {
		t.Error("PolicyScope is convertible to Scope: the two types must be kept apart")
	}
}
