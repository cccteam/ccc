package org

import (
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// regionsOf is a placement's two regions by name, the primary first.
func regionsOf(primary, secondary string) []Region {
	return []Region{{Name: primary, Code: "aaa"}, {Name: secondary, Code: "bbb"}}
}

// TestSpannerConfigs holds the shared instance's configuration (spanner.config) to the
// table: every configuration bedrock knows is accepted for its two read-write regions in
// either order, and refused for another pair with the configurations that fit named; a
// configuration the table lacks is refused with the same list, and a pair no configuration
// has says so.
func TestSpannerConfigs(t *testing.T) {
	t.Parallel()

	const table = "(nam10: us-central1 and us-west3; nam6 and nam11: us-central1 and us-east1; nam7, nam12 and nam16: us-central1 and us-east4)"
	tests := []struct {
		name        string
		primary     string
		secondary   string
		config      string
		wantFitting []string
		wantErr     string
	}{
		{name: "nam10 for us-central1 and us-west3", primary: "us-central1", secondary: "us-west3", config: "nam10", wantFitting: []string{"nam10"}},
		{name: "nam10 with the regions in the other order", primary: "us-west3", secondary: "us-central1", config: "nam10", wantFitting: []string{"nam10"}},
		{name: "nam6 for us-central1 and us-east1", primary: "us-central1", secondary: "us-east1", config: "nam6", wantFitting: []string{"nam6", "nam11"}},
		{name: "nam11 for us-central1 and us-east1", primary: "us-central1", secondary: "us-east1", config: "nam11", wantFitting: []string{"nam6", "nam11"}},
		{name: "nam7 for us-central1 and us-east4", primary: "us-central1", secondary: "us-east4", config: "nam7", wantFitting: []string{"nam7", "nam12", "nam16"}},
		{name: "nam12 for us-central1 and us-east4", primary: "us-central1", secondary: "us-east4", config: "nam12", wantFitting: []string{"nam7", "nam12", "nam16"}},
		{name: "nam16 for us-central1 and us-east4", primary: "us-central1", secondary: "us-east4", config: "nam16", wantFitting: []string{"nam7", "nam12", "nam16"}},
		{
			name: "nam10 for us-central1 and us-east1 is refused, naming nam6 and nam11", primary: "us-central1", secondary: "us-east1", config: "nam10",
			wantErr: `spanner.config "nam10" has its read-write replicas in us-central1 and us-west3, not in the placement's regions us-central1 and us-east1: the configurations whose read-write replicas are in us-central1 and us-east1 are nam6 and nam11`,
		},
		{
			name: "nam6 for us-central1 and us-west3 is refused, naming nam10", primary: "us-central1", secondary: "us-west3", config: "nam6",
			wantErr: `spanner.config "nam6" has its read-write replicas in us-central1 and us-east1, not in the placement's regions us-central1 and us-west3: the configuration whose read-write replicas are in us-central1 and us-west3 is nam10`,
		},
		{
			name: "nam16 for us-central1 and us-east1 is refused", primary: "us-central1", secondary: "us-east1", config: "nam16",
			wantErr: `spanner.config "nam16" has its read-write replicas in us-central1 and us-east4, not in the placement's regions us-central1 and us-east1: the configurations whose read-write replicas are in us-central1 and us-east1 are nam6 and nam11`,
		},
		{
			name: "a configuration the table lacks is refused with the same list", primary: "us-central1", secondary: "us-east4", config: "nam3",
			wantErr: `spanner.config "nam3" is not a multi-region configuration bedrock knows ` + table + `: the configurations whose read-write replicas are in us-central1 and us-east4 are nam7, nam12 and nam16`,
		},
		{
			name: "a regional configuration for the shared instance is refused", primary: "us-central1", secondary: "us-west3", config: "regional-us-central1",
			wantErr: `spanner.config "regional-us-central1" is not a multi-region configuration bedrock knows ` + table + `: the configuration whose read-write replicas are in us-central1 and us-west3 is nam10`,
		},
		{
			name: "two regions no configuration has", primary: "us-east1", secondary: "us-west1", config: "nam10",
			wantErr: "no configuration bedrock knows has its read-write replicas in us-east1 and us-west1, so the regions are chosen with the configuration",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			p.Regions = regionsOf(tt.primary, tt.secondary)
			p.Spanner.Config = tt.config
			err := p.Validate()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Validate() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if got := p.FittingSpannerConfigs(); !slices.Equal(got, tt.wantFitting) {
				t.Errorf("FittingSpannerConfigs() = %v, want %v", got, tt.wantFitting)
			}
		})
	}
}

// allTrue is OpenTofu's alltrue, for evaluating a rendered validation.
var allTrue = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "list", Type: cty.List(cty.Bool)}},
	Type:   function.StaticReturnType(cty.Bool),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		for _, v := range args[0].AsValueSlice() {
			if !v.True() {
				return cty.False, nil
			}
		}

		return cty.True, nil
	},
})

// ownInstanceCondition is the condition of 2-env's spanner_instances validation that
// holds an own instance's configuration to the regions, from the rendered variables.tf.
func ownInstanceCondition(t *testing.T, p *Placement) hcl.Expression {
	t.Helper()

	files, err := Render(p)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	for _, f := range files {
		if f.Path != envLayer+"/variables.tf" {
			continue
		}
		file, diags := hclparse.NewParser().ParseHCL(f.Content, f.Path)
		if diags.HasErrors() {
			t.Fatalf("%s: %s", f.Path, diags.Error())
		}
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			t.Fatalf("%s: not native syntax", f.Path)
		}
		for _, b := range body.Blocks {
			if b.Type != "variable" || len(b.Labels) != 1 || b.Labels[0] != "spanner_instances" {
				continue
			}
			for _, v := range b.Body.Blocks {
				cond, ok := v.Body.Attributes["condition"]
				if v.Type == "validation" && ok && strings.Contains(string(cond.SrcRange.SliceBytes(f.Content)), "coalesce") {
					return cond.Expr
				}
			}
		}
	}
	t.Fatal("2-env/variables.tf has no validation of an own instance's configuration")

	return nil
}

// TestOwnInstanceConfig evaluates 2-env's validation of an environment's own instance:
// regional in the primary region, where the primary service runs, or multi-region on a
// configuration whose read-write replicas are the two regions; a regional instance in the
// secondary region and a multi-region configuration for other regions are refused, and a
// shared environment's configuration is not looked at.
func TestOwnInstanceConfig(t *testing.T) {
	t.Parallel()

	unset := cty.NullVal(cty.String)
	tests := []struct {
		name      string
		secondary string
		placement string
		config    cty.Value
		want      bool
	}{
		{name: "unset: regional in the primary region", secondary: "us-west3", placement: "own", config: unset, want: true},
		{name: "regional in the primary region", secondary: "us-west3", placement: "own", config: cty.StringVal("regional-us-central1"), want: true},
		{name: "regional in the secondary region is refused", secondary: "us-west3", placement: "own", config: cty.StringVal("regional-us-west3"), want: false},
		{name: "regional in a region of neither is refused", secondary: "us-west3", placement: "own", config: cty.StringVal("regional-europe-west1"), want: false},
		{name: "the multi-region configuration that fits", secondary: "us-west3", placement: "own", config: cty.StringVal("nam10"), want: true},
		{name: "a multi-region configuration for other regions is refused", secondary: "us-west3", placement: "own", config: cty.StringVal("nam6"), want: false},
		{name: "each configuration that fits us-central1 and us-east4", secondary: "us-east4", placement: "own", config: cty.StringVal("nam16"), want: true},
		{name: "nam10 for us-central1 and us-east4 is refused", secondary: "us-east4", placement: "own", config: cty.StringVal("nam10"), want: false},
		{name: "a shared environment's configuration is not looked at", secondary: "us-west3", placement: "shared", config: cty.StringVal("regional-us-west3"), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := testPlacement(t)
			p.Regions[1].Name = tt.secondary
			p.Spanner.Config = p.FittingSpannerConfigs()[0]
			expr := ownInstanceCondition(t, p)
			instances := cty.MapVal(map[string]cty.Value{
				"tst": cty.ObjectVal(map[string]cty.Value{"placement": cty.StringVal(tt.placement), "config": tt.config}),
				"prd": cty.ObjectVal(map[string]cty.Value{"placement": cty.StringVal("shared"), "config": unset}),
			})
			ctx := &hcl.EvalContext{
				Variables: map[string]cty.Value{"var": cty.ObjectVal(map[string]cty.Value{"spanner_instances": instances})},
				Functions: map[string]function.Function{"alltrue": allTrue, "contains": stdlib.ContainsFunc, "coalesce": stdlib.CoalesceFunc},
			}
			got, diags := expr.Value(ctx)
			if diags.HasErrors() {
				t.Fatalf("condition: %s", diags.Error())
			}
			if got.True() != tt.want {
				t.Errorf("condition = %v, want %v", got.True(), tt.want)
			}
		})
	}
}
