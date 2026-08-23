package accesstypes

import (
	"testing"
)

func TestDomain_HasReservedMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		domain Domain
		want   bool
	}{
		{
			name:   "tenant name is marker-free",
			domain: Domain("station-alpha"),
		},
		{
			name:   "a tenant literally named global is ordinary data",
			domain: Domain("global"),
		},
		{
			name:   "the sentinel itself carries the marker",
			domain: GlobalDomain,
			want:   true,
		},
		{
			name:   "any other marker-bearing value is rejected material",
			domain: Domain("evil:tenant"),
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.domain.HasReservedMarker(); got != tt.want {
				t.Errorf("HasReservedMarker() = %v, want %v", got, tt.want)
			}
		})
	}
}
