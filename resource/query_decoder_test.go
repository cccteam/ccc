package resource

import (
	"net/url"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

type testResource struct {
	ID          string `spanner:"Id"`
	Description string `spanner:"Description"`
}

func (testResource) Resource() accesstypes.Resource {
	return "testResources"
}

func (testResource) DefaultConfig() Config {
	return Config{
		DBType: "spanner",
	}
}

type testRequest struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

func TestQueryDecoder_parseQuery_fields(t *testing.T) {
	t.Parallel()

	type args struct {
		rSet    *ResourceSet[testResource]
		columns url.Values
	}
	tests := []struct {
		name    string
		args    args
		want    []accesstypes.Field
		wantErr bool
	}{
		{
			name: "empty query",
			args: args{
				rSet: ccc.Must(NewResourceSet[testResource, testRequest](accesstypes.Read)),
			},
			want: nil,
		},
		{
			name: "columns with description",
			args: args{
				rSet:    ccc.Must(NewResourceSet[testResource, testRequest](accesstypes.Read)),
				columns: url.Values{"columns": []string{"description"}},
			},
			want: []accesstypes.Field{"Description"},
		},
		{
			name: "columns with invlaid column",
			args: args{
				rSet:    ccc.Must(NewResourceSet[testResource, testRequest](accesstypes.Read)),
				columns: url.Values{"columns": []string{"nonexistent"}},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, err := NewQueryDecoder[testResource, testRequest](tt.args.rSet)
			if (err != nil) != false {
				t.Fatalf("NewQueryDecoder() error = %v, wantErr %v", err, tt.wantErr)
			}

			got, _, err := d.parseQuery(tt.args.columns)
			if (err != nil) != tt.wantErr {
				t.Fatalf("fields() error = %v, wantErr %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("fields() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
