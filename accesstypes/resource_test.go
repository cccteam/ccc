package accesstypes

import (
	"testing"
)

func TestResourceFromStringAndBack(t *testing.T) {
	t.Parallel()

	type args struct {
		user string
	}
	tests := []struct {
		name string
		args args
		want Resource
	}{
		{
			name: "Administrator",
			args: args{
				user: "resource:Administrator",
			},
			want: "Administrator",
		},
		{
			name: "Bad",
			args: args{
				user: "resource:Bad",
			},
			want: Resource("Bad"),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := UnmarshalResource(tt.args.user)
			if got != tt.want {
				t.Errorf("ResourceFromString() = %v, want %v", got, tt.want)
			}
			if gotString := got.Marshal(); gotString != tt.args.user {
				t.Errorf("Resource.String() = %v, want %v", gotString, tt.args.user)
			}
		})
	}
}

func TestResource_Marshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		u         Resource
		want      string
		wantPanic bool
	}{
		{
			name: "Success",
			u:    "MyResource",
			want: resourcePrefix + "MyResource",
		},
		{
			name:      "Panic",
			u:         resourcePrefix + "MyResource",
			wantPanic: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				didPanic := recover()
				if tt.wantPanic != (didPanic != nil) {
					t.Errorf("Resource.Mashal() panic = %v, wantPanic %v", didPanic, tt.wantPanic)
				}
			}()
			if got := tt.u.Marshal(); got != tt.want {
				t.Errorf("Resource.Marshal() = %v, want %v", got, tt.want)
			}
		})
	}
}
