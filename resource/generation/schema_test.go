package generation

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_schemageneration(t *testing.T) {
	// To view the output swap the tempdir with a real dir relative to the generation package directory
	generator, err := NewSchemaGenerator("generation/testdata/schemagen/resources.go", t.TempDir(), t.TempDir(), 0, false)
	if err != nil {
		t.Error(err)

		return
	}

	if err := generator.Generate(); err != nil {
		t.Error(err)

		return
	}
}

func Test_referenceExpr(t *testing.T) {
	type args struct {
		expr string
	}

	tests := []struct {
		name        string
		args        args
		wantTable   string
		wantColumns string
	}{
		{
			name:        "extracts column names properly",
			args:        args{expr: "Economies(Id, Type)"},
			wantTable:   "Economies",
			wantColumns: "Id, Type",
		},
		{
			name:        "extracts column names from weirdly formatted string",
			args:        args{expr: "Businesses ( Class,Org ) "},
			wantTable:   "Businesses",
			wantColumns: "Class, Org",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotTable, gotColumns := parseReferenceExpression(tt.args.expr)

			if diff := cmp.Diff(tt.wantTable, gotTable); diff != "" {
				t.Errorf("newReferenceExpr() table mismatch (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tt.wantColumns, gotColumns); diff != "" {
				t.Errorf("newReferenceExpr() columns mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
