package accesstypes

import (
	"testing"
)

func TestResource_ResourceWithTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resource  Resource
		tag       Tag
		want      Resource
		wantPanic bool
	}{
		{
			name:     "field resource",
			resource: Resource("Persons"),
			tag:      Tag("firstName"),
			want:     Resource("Persons.firstName"),
		},
		{
			name:      "dotted tag panics",
			resource:  Resource("Persons"),
			tag:       Tag("first.name"),
			wantPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if r := recover(); (r != nil) != tt.wantPanic {
					t.Errorf("ResourceWithTag() panic = %v, wantPanic %v", r, tt.wantPanic)
				}
			}()

			if got := tt.resource.ResourceWithTag(tt.tag); got != tt.want {
				t.Errorf("ResourceWithTag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResource_HasReservedMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resource Resource
		want     bool
	}{
		{
			name:     "struct-derived name is marker-free",
			resource: Resource("Persons"),
		},
		{
			name:     "the sentinel itself carries the marker",
			resource: GlobalResource,
			want:     true,
		},
		{
			name:     "hand-written marker-bearing name is rejected material",
			resource: Resource("access:things"),
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.resource.HasReservedMarker(); got != tt.want {
				t.Errorf("HasReservedMarker() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResource_ResourceAndTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resource     Resource
		wantResource Resource
		wantTag      Tag
		wantPanic    bool
	}{
		{
			name:         "parent resource only",
			resource:     Resource("Persons"),
			wantResource: Resource("Persons"),
		},
		{
			name:         "field resource splits",
			resource:     Resource("Persons.firstName"),
			wantResource: Resource("Persons"),
			wantTag:      Tag("firstName"),
		},
		{
			name:      "more than one dot panics",
			resource:  Resource("Persons.first.name"),
			wantPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if r := recover(); (r != nil) != tt.wantPanic {
					t.Errorf("ResourceAndTag() panic = %v, wantPanic %v", r, tt.wantPanic)
				}
			}()

			gotResource, gotTag := tt.resource.ResourceAndTag()
			if gotResource != tt.wantResource || gotTag != tt.wantTag {
				t.Errorf("ResourceAndTag() = (%v, %v), want (%v, %v)", gotResource, gotTag, tt.wantResource, tt.wantTag)
			}
		})
	}
}
