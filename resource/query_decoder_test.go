package resource

import (
	"context"
	"net/url"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/mock/mock_accesstypes"
	"github.com/google/go-cmp/cmp"
	"go.uber.org/mock/gomock"
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

func TestQueryDecoder_fields(t *testing.T) {
	t.Parallel()

	type args struct {
		rSet  *ResourceSet[testResource, testRequest]
		query url.Values
	}
	tests := []struct {
		name    string
		args    args
		prepare func(enforcer *mock_accesstypes.MockEnforcer)
		want    []accesstypes.Field
		wantErr bool
	}{
		{
			name: "empty query",
			args: args{
				rSet:  ccc.Must(NewResourceSet[testResource, testRequest](accesstypes.Read)),
				query: url.Values{},
			},
			prepare: func(enforcer *mock_accesstypes.MockEnforcer) {
				enforcer.EXPECT().RequireResources(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil, nil).AnyTimes()
			},
			want: []accesstypes.Field{"ID", "Description"},
		},
		{
			name: "column with description",
			args: args{
				rSet:  ccc.Must(NewResourceSet[testResource, testRequest](accesstypes.Read)),
				query: url.Values{"columns": []string{"description"}},
			},
			prepare: func(enforcer *mock_accesstypes.MockEnforcer) {
				enforcer.EXPECT().RequireResources(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil, nil).AnyTimes()
			},
			want: []accesstypes.Field{"Description"},
		},
		{
			name: "column with description",
			args: args{
				rSet:  ccc.Must(NewResourceSet[testResource, testRequest](accesstypes.Read)),
				query: url.Values{"columns": []string{"nonexistent"}},
			},
			prepare: func(enforcer *mock_accesstypes.MockEnforcer) {
				enforcer.EXPECT().RequireResources(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil, nil).AnyTimes()
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			enforcer := mock_accesstypes.NewMockEnforcer(ctrl)
			tt.prepare(enforcer)

			d, err := NewQueryDecoder(tt.args.rSet, enforcer, func(context.Context) accesstypes.Domain { return "" }, func(context.Context) accesstypes.User { return "" })
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewQueryDecoder() error = %v, wantErr %v", err, tt.wantErr)
			}

			ctx, done := context.WithCancel(context.Background())
			defer done()

			got, err := d.fields(ctx, tt.args.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("fields() error = %v, wantErr %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("fields() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
