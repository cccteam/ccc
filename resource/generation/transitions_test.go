package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/google/go-cmp/cmp"
)

// resolveFixtureTransition runs the transition resolution on one fixture RPC
// struct the way structsToRPCMethods does: scope first, then the declaration
// against the client's resolved resources.
func resolveFixtureTransition(t *testing.T, c *client, structs map[string]*parser.Struct, name string) (*rpcMethodInfo, error) {
	t.Helper()

	s := structs[name]
	if s == nil {
		t.Fatalf("struct %q not found in fixture package", name)
	}
	annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
	if err != nil {
		t.Fatalf("ScanStruct(%s) error = %v", name, err)
	}
	rpcMethod := &rpcMethodInfo{Struct: s}
	if err := resolvePermissionScope(annotations, &rpcMethod.PermissionScope); err != nil {
		t.Fatalf("resolvePermissionScope(%s) error = %v", name, err)
	}

	return rpcMethod, c.resolveTransition(rpcMethod, s, annotations)
}

// transitionFixtureClient is the state fixture client with the workflow root
// (and the stateless Ship) resolved into c.resources, the shape the RPC
// extraction sees.
func transitionFixtureClient(t *testing.T, structs map[string]*parser.Struct) *client {
	t.Helper()

	c := stateFixtureClient()
	root := buildWorkflowFixture(t, c, structs, "StatefulTask")
	ship := buildWorkflowFixture(t, c, structs, "Ship")
	c.resources = []*resourceInfo{root, ship}

	return c
}

// TestResolveTransition pins the @transition/@target contract: the resolved
// declaration carries everything the handler frame renders from, and each
// malformed shape is rejected with the reason named.
func TestResolveTransition(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "bindingfixture"))

	t.Run("well-formed declaration resolves", func(t *testing.T) {
		t.Parallel()

		c := transitionFixtureClient(t, structs)
		rpcMethod, err := resolveFixtureTransition(t, c, structs, "ApproveTask")
		if err != nil {
			t.Fatalf("resolveTransition() error = %v", err)
		}
		want := &rpcTransition{
			rpcTarget: rpcTarget{
				RootStruct:   "StatefulTask",
				RootResource: "StatefulTasks",
				TargetField:  "TaskID",
				RootPKField:  "ID",
				RootPKColumn: "Id",
				StateField:   "State",
			},
			From: []string{"open"},
			To:   "approved",
		}
		if diff := cmp.Diff(want, rpcMethod.Transition, cmp.AllowUnexported(rpcTransition{})); diff != "" {
			t.Errorf("resolveTransition() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("multi-valued from set resolves in declaration order", func(t *testing.T) {
		t.Parallel()

		c := transitionFixtureClient(t, structs)
		rpcMethod, err := resolveFixtureTransition(t, c, structs, "CloseTask")
		if err != nil {
			t.Fatalf("resolveTransition() error = %v", err)
		}
		if got, want := rpcMethod.Transition.From, []string{"open", "approved"}; !cmp.Equal(want, got) {
			t.Errorf("From = %v, want %v", got, want)
		}
		if got, want := rpcMethod.Transition.FromCases(), `"open", "approved"`; got != want {
			t.Errorf("FromCases() = %q, want %q", got, want)
		}
	})

	t.Run("plain RPC resolves to no transition", func(t *testing.T) {
		t.Parallel()

		c := transitionFixtureClient(t, structs)
		plain, err := resolveFixtureTransition(t, c, structs, "StatefulTask")
		if err != nil {
			t.Fatalf("resolveTransition() on an unannotated struct error = %v", err)
		}
		if plain.Transition != nil {
			t.Errorf("unannotated struct resolved a transition: %+v", plain.Transition)
		}
		if plain.Target != nil {
			t.Errorf("unannotated struct resolved a target: %+v", plain.Target)
		}
	})

	t.Run("transition sets the target alias", func(t *testing.T) {
		t.Parallel()

		c := transitionFixtureClient(t, structs)
		rpcMethod, err := resolveFixtureTransition(t, c, structs, "ApproveTask")
		if err != nil {
			t.Fatalf("resolveTransition() error = %v", err)
		}
		if rpcMethod.Target == nil || rpcMethod.Target != &rpcMethod.Transition.rpcTarget {
			t.Errorf("Target = %+v, want the transition's embedded target", rpcMethod.Target)
		}
	})

	t.Run("plain @target(Root) resolves the located-row form", func(t *testing.T) {
		t.Parallel()

		c := transitionFixtureClient(t, structs)
		rpcMethod, err := resolveFixtureTransition(t, c, structs, "PlainTargetTask")
		if err != nil {
			t.Fatalf("resolveTransition(PlainTargetTask) error = %v", err)
		}
		if rpcMethod.Transition != nil {
			t.Errorf("plain target resolved a transition: %+v", rpcMethod.Transition)
		}
		want := &rpcTarget{
			RootStruct:   "StatefulTask",
			RootResource: "StatefulTasks",
			TargetField:  "TaskID",
			RootPKField:  "ID",
			RootPKColumn: "Id",
			// The plain form never touches state, even on a stateful root.
			StateField: "",
		}
		if diff := cmp.Diff(want, rpcMethod.Target); diff != "" {
			t.Errorf("resolveTransition(PlainTargetTask) target mismatch (-want +got):\n%s", diff)
		}
		if got, want := rpcMethod.Target.LocateColumnCalls(), ".ID()"; got != want {
			t.Errorf("LocateColumnCalls() = %q, want %q (the key itself when no state or tenant column is read)", got, want)
		}
	})

	rejections := []struct {
		name        string
		fixture     string
		wantContain string
	}{
		{name: "unknown root", fixture: "TransitionUnknownRoot", wantContain: "unknown resource struct"},
		{name: "stateless root", fixture: "TransitionStatelessRoot", wantContain: "carries no @state"},
		{name: "from outside the enum", fixture: "TransitionBadFrom", wantContain: `"bogus" is not a value`},
		{name: "duplicate from value", fixture: "TransitionDuplicateFrom", wantContain: "appears twice in the from set"},
		{name: "to outside the enum", fixture: "TransitionBadTo", wantContain: `"bogus" is not a value`},
		{name: "no target field", fixture: "TransitionNoTarget", wantContain: "exactly one @target"},
		{name: "two target fields", fixture: "TransitionTwoTargets", wantContain: "a method addresses exactly one target row"},
		{name: "target type mismatch", fixture: "TransitionTargetTypeMismatch", wantContain: "does not match the root key"},
		{name: "scope mismatch", fixture: "TransitionScopeMismatch", wantContain: "permission scopes differ"},
		{name: "bare target without transition", fixture: "TargetWithoutTransition", wantContain: "names the row resource the handler locates"},
		{name: "target argument with transition", fixture: "TransitionTargetWithArg", wantContain: "takes no argument"},
	}
	for _, tt := range rejections {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := transitionFixtureClient(t, structs)
			_, err := resolveFixtureTransition(t, c, structs, tt.fixture)
			if err == nil {
				t.Fatalf("resolveTransition(%s) expected an error containing %q, got nil", tt.fixture, tt.wantContain)
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("resolveTransition(%s) error = %q, want containing %q", tt.fixture, err, tt.wantContain)
			}
		})
	}

	t.Run("DBRunner form is rejected", func(t *testing.T) {
		t.Parallel()

		// TransitionNotTxnRunner has no RunsInTxn method, so Implements fails.
		c := transitionFixtureClient(t, structs)
		_, err := resolveFixtureTransition(t, c, structs, "TransitionNotTxnRunner")
		if err == nil || !strings.Contains(err.Error(), "requires the TxnRunner form") {
			t.Errorf("resolveTransition(TransitionNotTxnRunner) error = %v, want the TxnRunner requirement", err)
		}
	})

	t.Run("suppressed handler is rejected", func(t *testing.T) {
		t.Parallel()

		c := transitionFixtureClient(t, structs)
		s := structs["ApproveTask"]
		annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
		if err != nil {
			t.Fatalf("ScanStruct() error = %v", err)
		}
		rpcMethod := &rpcMethodInfo{Struct: s, SuppressHandler: true}
		err = c.resolveTransition(rpcMethod, s, annotations)
		if err == nil || !strings.Contains(err.Error(), "generated handler") {
			t.Errorf("resolveTransition() with a suppressed handler error = %v, want the generated-handler requirement", err)
		}
	})
}

// Test_rpcHandlerTemplate_transition pins the generated transition frame: the
// pre-body row location within the tenancy predicate, the pre-image state
// check, and the post-body stamp — and that a plain RPC method renders none
// of it.
func Test_rpcHandlerTemplate_transition(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "bindingfixture"))
	c := transitionFixtureClient(t, structs)

	buildMethod := func(t *testing.T, name string) *rpcMethodInfo {
		t.Helper()

		rpcMethod, err := resolveFixtureTransition(t, c, structs, name)
		if err != nil {
			t.Fatalf("resolveFixtureTransition(%s) error = %v", name, err)
		}
		for _, field := range rpcMethod.Struct.Fields() {
			rpcMethod.Fields = append(rpcMethod.Fields, &rpcField{Field: field})
		}

		return rpcMethod
	}

	render := func(t *testing.T, rpcMethod *rpcMethodInfo) string {
		t.Helper()

		out, err := c.generateTemplateOutput("rpcHandlerTemplate", rpcHandlerTemplate, &rpcHandlerData{
			Source:           "pkg/rpc",
			RPCMethod:        rpcMethod,
			Package:          "app",
			ApplicationName:  "App",
			ReceiverName:     "a",
			ResourcesPackage: "resources",
		})
		if err != nil {
			t.Fatalf("generateTemplateOutput() error = %v", err)
		}

		return string(out)
	}

	t.Run("declared transition renders the frame", func(t *testing.T) {
		t.Parallel()

		out := render(t, buildMethod(t, "CloseTask"))
		for _, want := range []string{
			"decoder := NewTargetedRPCDecoder[bindingfixture.CloseTask, request](a, accesstypes.Execute)",
			"params, gate, err := decoder.Decode(r, accesstypes.GlobalScope())",
			"resources.NewStatefulTaskQuery().",
			"AddColumns(resources.NewStatefulTaskColumns().State()).",
			"SetID(p.TaskID).",
			`httpio.NewNotFoundMessagef("StatefulTask %s does not exist", p.TaskID)`,
			"switch row.Data.State {",
			`case "open", "approved":`,
			// One refusal shape for the state check and the grant condition:
			// the wire never says which said no (§12).
			`httpio.NewForbiddenMessagef("CloseTask may not run against StatefulTask %s", p.TaskID)`,
			`gate.Enforce(ctx, txn, resource.ExecuteTarget{Resource: "StatefulTasks", Label: "StatefulTask", PKColumn: "Id"}, p.TaskID)`,
			`resources.NewStatefulTaskUpdatePatch(p.TaskID).SetState("closed").Buffer(ctx, txn, resource.UserEvent(ctx))`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("transition handler missing %q:\n%s", want, out)
			}
		}
		// A global-scoped root has no tenant key to compare.
		if strings.Contains(out, "TenantKeyEquals") {
			t.Errorf("global-scoped transition must not render a tenancy comparison:\n%s", out)
		}
	})

	t.Run("plain @target renders the located-row form", func(t *testing.T) {
		t.Parallel()

		out := render(t, buildMethod(t, "PlainTargetTask"))
		for _, want := range []string{
			"decoder := NewTargetedRPCDecoder[bindingfixture.PlainTargetTask, request](a, accesstypes.Execute)",
			"params, gate, err := decoder.Decode(r, accesstypes.GlobalScope())",
			"AddColumns(resources.NewStatefulTaskColumns().ID()).",
			`httpio.NewNotFoundMessagef("StatefulTask %s does not exist", p.TaskID)`,
			`gate.Enforce(ctx, txn, resource.ExecuteTarget{Resource: "StatefulTasks", Label: "StatefulTask", PKColumn: "Id"}, p.TaskID)`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("plain-target handler missing %q:\n%s", want, out)
			}
		}
		// No state check, no stamp: the plain form moves no state.
		for _, notWant := range []string{"switch row.Data", "UpdatePatch", "TenantKeyEquals"} {
			if strings.Contains(out, notWant) {
				t.Errorf("plain-target handler must not contain %q:\n%s", notWant, out)
			}
		}
	})

	t.Run("domain-scoped transition locates the row within the tenancy predicate", func(t *testing.T) {
		t.Parallel()

		rpcMethod := buildMethod(t, "CloseTask")
		rpcMethod.Transition.TenantField = "StationID"
		out := render(t, rpcMethod)
		for _, want := range []string{
			"AddColumns(resources.NewStatefulTaskColumns().State().StationID()).",
			"resource.TenantKeyEquals(row.Data.StationID, domain)",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("domain-scoped transition handler missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("plain RPC renders no frame", func(t *testing.T) {
		t.Parallel()

		rpcMethod := &rpcMethodInfo{Struct: structs["ApproveTask"]}
		for _, field := range rpcMethod.Struct.Fields() {
			rpcMethod.Fields = append(rpcMethod.Fields, &rpcField{Field: field})
		}
		out := render(t, rpcMethod)
		for _, notWant := range []string{"Declared target", "TenantKeyEquals", "UpdatePatch", "NotFoundMessagef", "gate", "Targeted"} {
			if strings.Contains(out, notWant) {
				t.Errorf("plain RPC handler must not contain %q:\n%s", notWant, out)
			}
		}
	})
}
