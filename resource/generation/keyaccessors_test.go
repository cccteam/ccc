package generation

import (
	"strings"
	"testing"
)

// Test_resourceFileTemplate_keyAccessors pins the accessor set the resource file
// template emits: a Set<Field> and <Field> pair on every primary-key column, a
// composite key's parts included, and on every column a single-column unique index
// covers; none on a column of a composite unique index, and none on a plain column.
func Test_resourceFileTemplate_keyAccessors(t *testing.T) {
	t.Parallel()

	c := &client{tableMap: bindingFixtureTables()}
	structs := fixtureStructs(loadFixture(t, "bindingfixture"))

	tests := []struct {
		name        string
		structName  string
		wantSetters []string
		noSetters   []string
	}{
		{
			name:        "a single-column primary key addresses the row",
			structName:  "UserProfile",
			wantSetters: []string{"UserID"},
			noSetters:   []string{"ApprovalLimit"},
		},
		{
			name:        "every part of a composite key addresses the row",
			structName:  "ScalarOnCompositeKey",
			wantSetters: []string{"GroupID", "UserID"},
			noSetters:   []string{"Seat"},
		},
		{
			name:        "a column of a composite unique index does not",
			structName:  "ScalarOnCompositeUnique",
			wantSetters: []string{"ID"},
			noSetters:   []string{"UserID", "GroupID", "Seat"},
		},
		{
			name:        "a null-filtered single-column unique index addresses the row",
			structName:  "NullFilteredAnchor",
			wantSetters: []string{"ID", "UserID"},
			noSetters:   []string{"ApprovalLimit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not found in fixture package", tt.structName)
			}
			table, err := c.tableMetadataFor(tt.structName)
			if err != nil {
				t.Fatalf("tableMetadataFor(%s) error = %v", tt.structName, err)
			}
			res := &resourceInfo{TypeInfo: s.TypeInfo, PkCount: table.PkCount}
			res.Fields, err = newResourceFields(res, s, table)
			if err != nil {
				t.Fatalf("newResourceFields(%s) error = %v", tt.structName, err)
			}

			r := &resourceGenerator{client: c}
			output, err := r.generateTemplateOutput("resourceFileTemplate", resourceFileTemplate, &resourceFileData{
				Source:   "fixture",
				Package:  "resources",
				Resource: res,
			})
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			rendered := string(output)

			for _, field := range tt.wantSetters {
				setter := "func (q *" + tt.structName + "Query) Set" + field + "("
				getter := "func (q *" + tt.structName + "Query) " + field + "()"
				if !strings.Contains(rendered, setter) || !strings.Contains(rendered, getter) {
					t.Errorf("rendered %s is missing the key accessors for %s", tt.structName, field)
				}
			}
			for _, field := range tt.noSetters {
				setter := "func (q *" + tt.structName + "Query) Set" + field + "("
				if strings.Contains(rendered, setter) {
					t.Errorf("rendered %s carries a key accessor for %s, which is not a row address", tt.structName, field)
				}
			}
		})
	}
}
