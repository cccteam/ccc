package patchset

import (
	"reflect"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestNewPrimaryKey(t *testing.T) {
	t.Parallel()

	type args struct {
		key   accesstypes.Field
		value any
	}
	tests := []struct {
		name string
		args args
		want PrimaryKey
	}{
		{
			name: "NewPrimaryKey",
			args: args{
				key:   "field1",
				value: "1",
			},
			want: PrimaryKey{
				keyParts: []KeyPart{
					{
						Key:   "field1",
						Value: "1",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NewPrimaryKey(tt.args.key, tt.args.value); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewPrimaryKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
