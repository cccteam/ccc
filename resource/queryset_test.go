package resource

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"go.uber.org/mock/gomock"
)

//go:generate mockgen -destination mock_resource_iface.go -package=resource . Reader

// SortTestResource is used for testing sorting functionality.
type SortTestResource struct {
	ID   string `spanner:"Id"   db:"Id"`
	Name string `spanner:"Name" db:"Name"`
	Date string `spanner:"Date" db:"Date"`
}

func (SortTestResource) Resource() accesstypes.Resource {
	return "SortTestResources"
}

func (s SortTestResource) DefaultConfig() Config {
	return Config{}
}

func TestQuerySet_Stmt_OrderBy_Limit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		sortFields           []SortField
		limit                *uint64
		wantQueryContains    string
		wantErrorMsgContains string
		wantErr              bool
		assertFunc           func(t *testing.T, sql string, _ string)
	}{
		{
			name:              "with limit",
			limit:             ccc.Ptr(uint64(10)),
			wantQueryContains: "LIMIT 10",
		},
		{
			name:       "no sort fields or limit",
			sortFields: []SortField{},
			assertFunc: func(t *testing.T, sql string, _ string) {
				if strings.Contains(sql, "ORDER BY") {
					t.Errorf("Expected SQL NOT to contain 'ORDER BY', but got: %s", sql)
				}
				if strings.Contains(sql, "LIMIT") {
					t.Errorf("Expected SQL NOT to contain 'LIMIT', but got: %s", sql)
				}
			},
		},
		{
			name:              "single field ascending",
			sortFields:        []SortField{{Field: "Name", Direction: SortAscending}},
			wantQueryContains: "ORDER BY `Name` ASC",
		},
		{
			name:              "single field descending",
			sortFields:        []SortField{{Field: "Date", Direction: SortDescending}},
			wantQueryContains: "ORDER BY `Date` DESC",
		},
		{
			name:              "multiple fields mixed directions",
			sortFields:        []SortField{{Field: "Name", Direction: SortAscending}, {Field: "Date", Direction: SortDescending}},
			wantQueryContains: "ORDER BY `Name` ASC, `Date` DESC",
		},
		{
			name:              "sorting by ID field",
			sortFields:        []SortField{{Field: "ID", Direction: SortDescending}},
			wantQueryContains: "ORDER BY `Id` DESC",
		},
		{
			name:                 "invalid sort field",
			sortFields:           []SortField{{Field: "InvalidField", Direction: SortAscending}},
			wantErr:              true,                             // Expect error from buildOrderByClause
			wantErrorMsgContains: "not found in resource metadata", // buildOrderByClause error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rMeta := NewMetadata[SortTestResource]()

			qSet := NewQuerySet(rMeta)
			qSet.AddField("ID")

			qSet.SetSortFields(tt.sortFields)
			if tt.limit != nil {
				qSet.SetLimit(tt.limit)
			}
			stmt, err := qSet.stmt(SpannerDBType)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected an error, but got none")
				} else if tt.wantErrorMsgContains != "" && !strings.Contains(err.Error(), tt.wantErrorMsgContains) {
					t.Errorf("SpannerStmt() error = %v, want error message containing %q", err, tt.wantErrorMsgContains)
				}

				return // Test finished if error was expected
			}
			if err != nil {
				t.Fatalf("SpannerStmt() error = %v, wantErr %v", err, tt.wantErr)
			}

			sql := stmt.SQL // Access as a field, not a function
			if tt.assertFunc != nil {
				tt.assertFunc(t, sql, tt.wantQueryContains)
			} else if !strings.Contains(sql, tt.wantQueryContains) {
				t.Errorf("SpannerStmt() SQL = \n%s\nWant to contain:\n%s", sql, tt.wantQueryContains)
			}
		})
	}
}

func TestQuerySet_BatchList(t *testing.T) {
	t.Parallel()

	sourceResources := []*SortTestResource{
		{ID: "1", Name: "Resource 1"},
		{ID: "2", Name: "Resource 2"},
		{ID: "3", Name: "Resource 3"},
		{ID: "4", Name: "Resource 4"},
		{ID: "5", Name: "Resource 5"},
		{ID: "6", Name: "Resource 6"},
		{ID: "7", Name: "Resource 7"},
		{ID: "8", Name: "Resource 8"},
	}

	tests := []struct {
		name    string
		prepare func(ctrl *gomock.Controller) Client
		qSet    *QuerySet[SortTestResource]

		batchSize             int
		expectError           bool
		expectedErrorContains string
	}{
		{
			name: "batch size (3) not evenly divisable with without loss",
			prepare: func(ctrl *gomock.Controller) Client {
				sortTestResource := NewMockReader[SortTestResource](ctrl)
				sortTestResource.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
				sortTestResource.EXPECT().List(gomock.Any(), gomock.Any()).Return(
					MockIterSeq2(nil, sourceResources...),
				)

				return NewMockClient(nil, []any{sortTestResource}, nil)
			},
			qSet: func() *QuerySet[SortTestResource] {
				rMeta := NewMetadata[SortTestResource]()
				qSet := NewQuerySet(rMeta)
				qSet.AddField("ID")
				qSet.AddField("Name")

				return qSet
			}(),
			batchSize: 3,
		},
		{
			name: "batch size (1) evenly divisable without loss",
			prepare: func(ctrl *gomock.Controller) Client {
				sortTestResource := NewMockReader[SortTestResource](ctrl)
				sortTestResource.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
				sortTestResource.EXPECT().List(gomock.Any(), gomock.Any()).Return(
					MockIterSeq2(nil, sourceResources...),
				)

				return NewMockClient(nil, []any{sortTestResource}, nil)
			},
			qSet: func() *QuerySet[SortTestResource] {
				rMeta := NewMetadata[SortTestResource]()
				qSet := NewQuerySet(rMeta)
				qSet.AddField("ID")
				qSet.AddField("Name")

				return qSet
			}(),
			batchSize: 1,
		},
		{
			name: "batch size (2) evenly divisable without loss",
			prepare: func(ctrl *gomock.Controller) Client {
				sortTestResource := NewMockReader[SortTestResource](ctrl)
				sortTestResource.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
				sortTestResource.EXPECT().List(gomock.Any(), gomock.Any()).Return(
					MockIterSeq2(nil, sourceResources...),
				)

				return NewMockClient(nil, []any{sortTestResource}, nil)
			},
			qSet: func() *QuerySet[SortTestResource] {
				rMeta := NewMetadata[SortTestResource]()
				qSet := NewQuerySet(rMeta)
				qSet.AddField("ID")
				qSet.AddField("Name")

				return qSet
			}(),
			batchSize: 1,
		},
		{
			name: "batch size (4) evenly divisable without loss",
			prepare: func(ctrl *gomock.Controller) Client {
				sortTestResource := NewMockReader[SortTestResource](ctrl)
				sortTestResource.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
				sortTestResource.EXPECT().List(gomock.Any(), gomock.Any()).Return(
					MockIterSeq2(nil, sourceResources...),
				)

				return NewMockClient(nil, []any{sortTestResource}, nil)
			},
			qSet: func() *QuerySet[SortTestResource] {
				rMeta := NewMetadata[SortTestResource]()
				qSet := NewQuerySet(rMeta)
				qSet.AddField("ID")
				qSet.AddField("Name")

				return qSet
			}(),
			batchSize: 1,
		},
		{
			name: "batch size (8) evenly divisable without loss",
			prepare: func(ctrl *gomock.Controller) Client {
				sortTestResource := NewMockReader[SortTestResource](ctrl)
				sortTestResource.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
				sortTestResource.EXPECT().List(gomock.Any(), gomock.Any()).Return(
					MockIterSeq2(nil, sourceResources...),
				)

				return NewMockClient(nil, []any{sortTestResource}, nil)
			},
			qSet: func() *QuerySet[SortTestResource] {
				rMeta := NewMetadata[SortTestResource]()
				qSet := NewQuerySet(rMeta)
				qSet.AddField("ID")
				qSet.AddField("Name")

				return qSet
			}(),
			batchSize: 1,
		},
		{
			name: "batch size (10) larger then data without loss",
			prepare: func(ctrl *gomock.Controller) Client {
				sortTestResource := NewMockReader[SortTestResource](ctrl)
				sortTestResource.EXPECT().DBType().MinTimes(1).Return(SpannerDBType)
				sortTestResource.EXPECT().List(gomock.Any(), gomock.Any()).Return(
					MockIterSeq2(nil, sourceResources...),
				)

				return NewMockClient(nil, []any{sortTestResource}, nil)
			},
			qSet: func() *QuerySet[SortTestResource] {
				rMeta := NewMetadata[SortTestResource]()
				qSet := NewQuerySet(rMeta)
				qSet.AddField("ID")
				qSet.AddField("Name")

				return qSet
			}(),
			batchSize: 1,
		},
		{
			name: "returns an error for invalid batch size",
			prepare: func(ctrl *gomock.Controller) Client {
				sortTestResource := NewMockReader[SortTestResource](ctrl)

				return NewMockClient(nil, []any{sortTestResource}, nil)
			},
			qSet: func() *QuerySet[SortTestResource] {
				rMeta := NewMetadata[SortTestResource]()
				qSet := NewQuerySet(rMeta)

				return qSet
			}(),
			batchSize:             0,
			expectError:           true,
			expectedErrorContains: "invalid batch size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			txn := tt.prepare(ctrl)

			var collectedResources []*SortTestResource
			for batch := range tt.qSet.BatchList(t.Context(), txn, tt.batchSize) {
				for resource, err := range batch {
					if tt.expectError {
						if err == nil {
							t.Fatal("Expected an error but got nil")
						}
						if !strings.Contains(err.Error(), tt.expectedErrorContains) {
							t.Errorf("Expected error message to contain '%s', but got: %v", tt.expectedErrorContains, err)
						}

						return // Stop processing after finding the expected error
					}
					if err != nil {
						t.Fatalf("Unexpected error while iterating a batch: %v", err)
					}
					collectedResources = append(collectedResources, resource)
				}
			}

			if !tt.expectError {
				if len(collectedResources) != len(sourceResources) {
					t.Fatalf("Expected %d resources, but got %d", len(sourceResources), len(collectedResources))
				}
				for i, res := range collectedResources {
					if *res != *sourceResources[i] {
						t.Errorf("Resource at index %d does not match. Got %+v, want %+v", i, *res, *sourceResources[i])
					}
				}
			}
		})
	}
}
