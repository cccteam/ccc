package cli

import (
	"reflect"
	"testing"
)

// TestRootClientsWireEverySeam holds the root organization's clients, as the command tree
// wires them over real credentials, to the engine's seams: every one a function the engine
// may call is wired, so a seam added to the engine is wired here in the same change. The
// fakes the engine's tests run over wire everything, which is how a run in the lab found
// the budgets and BigQuery seams unwired while every test passed. Create is set per run
// (the apply's creation seams), and Now defaults to the clock when nil.
func TestRootClientsWireEverySeam(t *testing.T) {
	t.Parallel()

	perRun := map[string]bool{"Create": true, "Now": true}
	v := reflect.ValueOf(rootClients()).Elem()
	for i := range v.NumField() {
		f := v.Type().Field(i)
		if perRun[f.Name] {
			continue
		}
		if v.Field(i).IsNil() {
			t.Errorf("rootClients() leaves %s nil: the engine's seam is not wired for a run over real credentials", f.Name)
		}
	}
}
