package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

func TestResolveOrder(t *testing.T) {
	t.Parallel()

	sortable := func(field string) error {
		switch field {
		case "Deadline", "Hazard", "ID":
			return nil
		default:
			return errors.Newf("%s is not a field", field)
		}
	}

	tests := []struct {
		name       string
		annotation string
		want       []resource.SortField
		wantErr    string
	}{
		{
			name:       "fields with directions",
			annotation: "Deadline asc, Hazard desc",
			want:       []resource.SortField{{Field: "Deadline", Direction: resource.SortAscending}, {Field: "Hazard", Direction: resource.SortDescending}},
		},
		{
			name:       "direction defaults to ascending",
			annotation: "Deadline",
			want:       []resource.SortField{{Field: "Deadline", Direction: resource.SortAscending}},
		},
		{
			name:       "the primary key may be named",
			annotation: "Hazard desc, ID desc",
			want:       []resource.SortField{{Field: "Hazard", Direction: resource.SortDescending}, {Field: "ID", Direction: resource.SortDescending}},
		},
		{
			name:       "unknown field is refused",
			annotation: "Fee",
			wantErr:    "Fee is not a field",
		},
		{
			name:       "bad direction is refused naming the field",
			annotation: "Deadline up",
			wantErr:    `direction "up" on Deadline must be asc or desc`,
		},
		{
			name:       "a field named twice is refused",
			annotation: "Deadline, Deadline desc",
			wantErr:    "names Deadline twice",
		},
		{
			name:       "an entry with too many words is refused",
			annotation: "Deadline asc first",
			wantErr:    "must be a field name followed by an optional asc or desc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveOrder(genlang.Arg(tt.annotation), sortable)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveOrder() error = %v, want error containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveOrder() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("resolveOrder() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPagingDecl_PagingOption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		decl pagingDecl
		want string
	}{
		{
			name: "nothing declared emits nothing",
		},
		{
			name: "declared order",
			decl: pagingDecl{DeclaredOrder: []resource.SortField{{Field: "Deadline", Direction: resource.SortAscending}, {Field: "Hazard", Direction: resource.SortDescending}}},
			want: ".\n\t\tWithPaging(resource.Paging{Order: []resource.SortField{{Field: \"Deadline\", Direction: resource.SortAscending}, {Field: \"Hazard\", Direction: resource.SortDescending}}})",
		},
		{
			name: "declared page sizes",
			decl: pagingDecl{PageDefault: 25, PageMax: 200},
			want: ".\n\t\tWithPaging(resource.Paging{DefaultLimit: 25, MaxLimit: 200})",
		},
		{
			name: "order and sizes together",
			decl: pagingDecl{DeclaredOrder: []resource.SortField{{Field: "Deadline", Direction: resource.SortAscending}}, PageMax: 200},
			want: ".\n\t\tWithPaging(resource.Paging{Order: []resource.SortField{{Field: \"Deadline\", Direction: resource.SortAscending}}, MaxLimit: 200})",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.decl.PagingOption(); got != tt.want {
				t.Errorf("PagingOption() = %q, want %q", got, tt.want)
			}
		})
	}
}
