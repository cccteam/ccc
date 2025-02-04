package generation

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_formatResourceInterfaceTypes(t *testing.T) {
	t.Parallel()

	type args struct {
		types []*generatedType
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "empty",
			args: args{
				types: []*generatedType{},
			},
			want: "",
		},
		{
			name: "One type",
			args: args{
				types: []*generatedType{
					{Name: "Resource1"},
				},
			},
			want: "\tResource1",
		},
		{
			name: "many type",
			args: args{
				types: []*generatedType{
					{Name: "Resource1"},
					{Name: "MyResource1"},
					{Name: "YourResource1"},
					{Name: "Resource2"},
					{Name: "Resource3"},
					{Name: "Resource4"},
					{Name: "Resource5"},
					{Name: "Resource6"},
					{Name: "Resource7"},
					{Name: "Resource8"},
					{Name: "Resource9"},
				},
			},
			want: "\tResource1 | MyResource1 | YourResource1 | Resource2 | Resource3 | Resource4 | Resource5 | Resource6 | \n\tResource7 | Resource8 | Resource9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatResourceInterfaceTypes(tt.args.types)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("formatResourceInterfaceTypes() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
