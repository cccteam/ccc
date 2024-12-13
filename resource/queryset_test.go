package resource

import (
	"github.com/cccteam/ccc/accesstypes"
)

type SpannerStruct struct {
	Field1 string `spanner:"field1"`
	Field2 string `spanner:"fieldtwo"`
	Field3 int    `spanner:"field3"`
	Field5 string `spanner:"field5"`
	Field4 string `spanner:"field4"`
}

func (SpannerStruct) Resource() accesstypes.Resource {
	return "SpannerStructs"
}

// func TestQuerySet_Columns(t *testing.T) {
// 	t.Parallel()

// 	type args struct {
// 		patchSet *resource.PatchSet
// 	}
// 	tests := []struct {
// 		name string
// 		args args
// 		want Columns
// 	}{
// 		{
// 			name: "multiple fields in patchSet",
// 			args: args{
// 				patchSet: resource.NewPatchSet(resource.NewRow[SpannerStruct]()).
// 					Set("Field2", "apple").
// 					Set("Field3", 10),
// 			},
// 			want: "fieldtwo, field3",
// 		},
// 		{
// 			name: "multiple fields not in sorted order",
// 			args: args{
// 				patchSet: resource.NewPatchSet(resource.NewRow[SpannerStruct]()).
// 					Set("Field4", "apple").
// 					Set("Field5", "bannana"),
// 			},
// 			want: "field5, field4",
// 		},
// 	}
// 	for _, tt := range tests {
// 		tt := tt
// 		t.Run(tt.name, func(t *testing.T) {
// 			t.Parallel()

// 			got, _ := tm.Columns(tt.args.patchSet.QuerySet())
// 			if got != tt.want {
// 				t.Errorf("Patcher.Columns() = (%v),  want (%v)", got, tt.want)
// 			}
// 		})
// 	}
// }

// type PostgresStruct struct {
// 	Field1 string `db:"field1"`
// 	Field2 string `db:"fieldtwo"`
// 	Field3 int    `db:"field3"`
// 	Field5 string `db:"field5"`
// 	Field4 string `db:"field4"`
// }

// func (PostgresStruct) Resource() accesstypes.Resource {
// 	return "PostgresStructs"
// }

// func TestPatcher_Postgres_Columns(t *testing.T) {
// 	t.Parallel()

// 	type args struct {
// 		patchSet *resource.PatchSet
// 	}
// 	tests := []struct {
// 		name string
// 		args args
// 		want Columns
// 	}{
// 		{
// 			name: "multiple fields in patchSet",
// 			args: args{
// 				patchSet: resource.NewPatchSet(resource.NewRow[PostgresStruct]()).
// 					Set("Field2", "apple").
// 					Set("Field3", 10),
// 			},
// 			want: `"fieldtwo", "field3"`,
// 		},
// 		{
// 			name: "multiple fields not in sorted order",
// 			args: args{
// 				patchSet: resource.NewPatchSet(resource.NewRow[PostgresStruct]()).
// 					Set("Field4", "apple").
// 					Set("Field5", "bannana"),
// 			},
// 			want: `"field5", "field4"`,
// 		},
// 	}
// 	for _, tt := range tests {
// 		tt := tt
// 		t.Run(tt.name, func(t *testing.T) {
// 			t.Parallel()

// 			got, _ := tm.Columns(tt.args.patchSet.QuerySet())
// 			if got != tt.want {
// 				t.Errorf("Patcher.Columns() = (%v),  want (%v)", got, tt.want)
// 			}
// 		})
// 	}
// }
