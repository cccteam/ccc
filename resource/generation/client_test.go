package generation

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_formatResourceInterfaceTypes(t *testing.T) {
	t.Parallel()

	type args struct {
		types []*resourceInfo
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "empty",
			args: args{
				types: []*resourceInfo{},
			},
			want: "",
		},
		{
			name: "One type",
			args: args{
				types: []*resourceInfo{
					{parsedType: parsedType{name: "Resource1"}},
				},
			},
			want: "\tResource1",
		},
		{
			name: "many type",
			args: args{
				types: []*resourceInfo{
					{parsedType: parsedType{name: "Resource1"}},
					{parsedType: parsedType{name: "MyResource1"}},
					{parsedType: parsedType{name: "YourResource1"}},
					{parsedType: parsedType{name: "Resource2"}},
					{parsedType: parsedType{name: "Resource3"}},
					{parsedType: parsedType{name: "Resource4"}},
					{parsedType: parsedType{name: "Resource5"}},
					{parsedType: parsedType{name: "Resource6"}},
					{parsedType: parsedType{name: "Resource7"}},
					{parsedType: parsedType{name: "Resource8"}},
					{parsedType: parsedType{name: "Resource9"}},
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

func Test_searchExpressionFields(t *testing.T) {
	t.Parallel()
	type args struct {
		expression string
		cols       map[string]columnMeta
	}
	tests := []struct {
		name    string
		args    args
		want    []*expressionField
		wantErr bool
	}{
		{
			name: "success multi line",
			args: args{
				expression: `TOKENLIST_CONCAT([
								(TOKENIZE_SUBSTRING(FirstName)),
								(TOKENIZE_SUBSTRING(LastName)),
								(TOKENIZE_SUBSTRING(FormerLastName)),
								(TOKENIZE_SUBSTRING(SUBSTR(Ssn, -4))),
								(TOKENIZE_SUBSTRING(Ssn))
							])`,
				cols: map[string]columnMeta{
					"FirstName":      {},
					"LastName":       {},
					"FormerLastName": {},
					"Ssn":            {},
				},
			},
			want: []*expressionField{
				{tokenType: "substring", fieldName: "FirstName"},
				{tokenType: "substring", fieldName: "LastName"},
				{tokenType: "substring", fieldName: "FormerLastName"},
				{tokenType: "substring", fieldName: "Ssn"},
			},
		},
		{
			name: "success single line",
			args: args{
				expression: "TOKENLIST_CONCAT([(TOKENIZE_SUBSTRING(FirstName)),(TOKENIZE_SUBSTRING(LastName)),(TOKENIZE_SUBSTRING(FormerLastName)),(TOKENIZE_SUBSTRING(SUBSTR(Ssn, -4))),(TOKENIZE_SUBSTRING(Ssn))])",
				cols: map[string]columnMeta{
					"FirstName":      {},
					"LastName":       {},
					"FormerLastName": {},
					"Ssn":            {},
				},
			},
			want: []*expressionField{
				{tokenType: "substring", fieldName: "FirstName"},
				{tokenType: "substring", fieldName: "LastName"},
				{tokenType: "substring", fieldName: "FormerLastName"},
				{tokenType: "substring", fieldName: "Ssn"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := searchExpressionFields(tt.args.expression, tt.args.cols)
			if (err != nil) != tt.wantErr {
				t.Fatalf("searchExpressionFields() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(expressionField{})); diff != "" {
				t.Errorf("searchExpressionFields() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
