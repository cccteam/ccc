package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser/genlang"
)

// Test_validatePickerSources pins the rule that a bounded picker source serves a
// read: a field whose picker lists a resource with a @page maximum and a suppressed
// read is refused naming the two ways out, on the declared path and on the foreign
// key path; a source with no maximum may suppress its read, a source that reads may
// declare a maximum, an unrouted foreign key target is no picker source, and an
// enumeration table rides inline.
func Test_validatePickerSources(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	arg := genlang.Arg("Curios")

	type source struct {
		pageMax   uint64
		suppress  bool
		unrouted  bool
		computed  bool
		enumTable bool
	}
	tests := []struct {
		name       string
		source     source
		declared   bool // the field declares @enumerate(Curios); else it is a foreign key into Curios
		onComputed bool // the declaring field sits on a computed resource
		wantErr    string
	}{
		{
			name:     "a declared source with a maximum and no read is refused naming the two ways out",
			source:   source{pageMax: 200, suppress: true},
			declared: true,
			wantErr:  "struct Widget field Name: the picker lists Curios, which serves at most 200 rows per page and no read; a paged picker reads the chosen row by key, so keep the read handler of Curios or drop its @page maximum",
		},
		{
			name:    "a foreign key into a bounded target with no read is refused the same way",
			source:  source{pageMax: 200, suppress: true},
			wantErr: "struct Widget field Name: the picker lists Curios, which serves at most 200 rows per page and no read",
		},
		{
			name:       "a computed field's declared source is checked too",
			source:     source{pageMax: 200, suppress: true},
			declared:   true,
			onComputed: true,
			wantErr:    "struct Summary field Total: the picker lists Curios, which serves at most 200 rows per page and no read",
		},
		{
			name:     "a bounded computed source with no read is refused",
			source:   source{pageMax: 50, suppress: true, computed: true},
			declared: true,
			wantErr:  "struct Widget field Name: the picker lists Curios, which serves at most 50 rows per page and no read",
		},
		{
			name:     "a source with no maximum is read whole and may suppress its read",
			source:   source{suppress: true},
			declared: true,
		},
		{
			name:     "a bounded source that reads is fine",
			source:   source{pageMax: 200},
			declared: true,
		},
		{
			name:   "a foreign key into an unrouted resource is no picker source",
			source: source{pageMax: 200, suppress: true, unrouted: true},
		},
		{
			name:     "an enumeration table rides inline and is never paged",
			source:   source{pageMax: 200, suppress: true, enumTable: true},
			declared: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{enumerateTables: map[string]string{}}
			if tt.source.enumTable {
				c.enumerateTables["Curios"] = "Curio"
			}
			if tt.source.computed {
				curio := fixtureComputedResource(t, structs, "Curio")
				curio.PageMax = tt.source.pageMax
				curio.SuppressReadHandler = tt.source.suppress
				c.computedResources = append(c.computedResources, curio)
			} else {
				c.resources = append(c.resources, fixtureResource(t, structs, "Curio", func(res *resourceInfo) {
					res.IsVirtual = true
					res.PageMax = tt.source.pageMax
					if tt.source.suppress {
						res.SuppressedHandlers = []HandlerType{ReadHandler}
					}
					if tt.source.unrouted {
						res.SuppressedRoutes = []RouteType{AllRoutes}
					}
				}))
			}

			var resources []*resourceInfo
			var computed []*computedResource
			if tt.onComputed {
				summary := fixtureComputedResource(t, structs, "Summary")
				summary.Fields[1].applyEnumeration(enumerationSource{Name: "Curios"})
				computed = append(computed, summary)
			} else {
				resources = append(resources, fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
					field := res.Fields[1]
					if tt.declared {
						field.enumerateArg = &arg
						field.applyEnumeration(enumerationSource{Name: "Curios"})
					} else {
						field.IsForeignKey = true
						field.ReferencedResource = "Curios"
					}
				}))
			}

			err := c.validatePickerSources(resources, computed)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validatePickerSources() error = %v, want none", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validatePickerSources() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// Test_resourceEndpoints pins the handler set a resource generates: a table lists,
// reads, and patches (no patch under the consolidated handler); a view lists, and
// reads when it declares its key; suppression removes what it names.
func Test_resourceEndpoints(t *testing.T) {
	t.Parallel()

	keyed := []*resourceField{{IsPrimaryKey: true}}

	tests := []struct {
		name string
		res  resourceInfo
		want []HandlerType
	}{
		{name: "a table", res: resourceInfo{PkCount: 1}, want: []HandlerType{ListHandler, ReadHandler, PatchHandler}},
		{name: "a consolidated table", res: resourceInfo{PkCount: 1, IsConsolidated: true}, want: []HandlerType{ListHandler, ReadHandler}},
		{name: "a view with a declared key lists and reads", res: resourceInfo{IsVirtual: true, Fields: keyed}, want: []HandlerType{ListHandler, ReadHandler}},
		{name: "a view with no key lists only", res: resourceInfo{IsVirtual: true}, want: []HandlerType{ListHandler}},
		{name: "a keyed view may suppress its read", res: resourceInfo{IsVirtual: true, Fields: keyed, SuppressedHandlers: []HandlerType{ReadHandler}}, want: []HandlerType{ListHandler}},
		{name: "a table may suppress its list", res: resourceInfo{PkCount: 1, SuppressedHandlers: []HandlerType{ListHandler}}, want: []HandlerType{ReadHandler, PatchHandler}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := resourceEndpoints(&tt.res)
			if strings.Join(handlerNames(got), ",") != strings.Join(handlerNames(tt.want), ",") {
				t.Errorf("resourceEndpoints() = %v, want %v", got, tt.want)
			}
		})
	}
}

func handlerNames(types []HandlerType) []string {
	names := make([]string, 0, len(types))
	for _, ht := range types {
		names = append(names, string(ht))
	}

	return names
}
