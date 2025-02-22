package resource

import (
	"context"
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
		rSet    *ResourceSet[testResource, testRequest]
		columns []accesstypes.Field
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
				rSet: ccc.Must(NewResourceSet[testResource, testRequest](accesstypes.Read)),
			},
			prepare: func(enforcer *mock_accesstypes.MockEnforcer) {
				enforcer.EXPECT().RequireResources(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil, nil).AnyTimes()
			},
			want: []accesstypes.Field{"ID", "Description"},
		},
		{
			name: "columns with description",
			args: args{
				rSet:    ccc.Must(NewResourceSet[testResource, testRequest](accesstypes.Read)),
				columns: []accesstypes.Field{"Description"},
			},
			prepare: func(enforcer *mock_accesstypes.MockEnforcer) {
				enforcer.EXPECT().RequireResources(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil, nil).AnyTimes()
			},
			want: []accesstypes.Field{"Description"},
		},
		{
			name: "columns with invlaid column",
			args: args{
				rSet:    ccc.Must(NewResourceSet[testResource, testRequest](accesstypes.Read)),
				columns: []accesstypes.Field{"nonexistent"},
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
			if (err != nil) != false {
				t.Fatalf("NewQueryDecoder() error = %v, wantErr %v", err, tt.wantErr)
			}

			ctx, done := context.WithCancel(context.Background())
			defer done()

			got, err := d.fields(ctx, tt.args.columns)
			if (err != nil) != tt.wantErr {
				t.Fatalf("fields() error = %v, wantErr %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("fields() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
