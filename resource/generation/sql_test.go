package generation

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_originTableName(t *testing.T) {
	t.Parallel()

	type args struct {
		sql        string
		columnName string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "empty sql",
			args: args{
				sql:        "",
				columnName: "foo",
			},
			wantErr: true,
		},
		{
			name: "empty columnName",
			args: args{
				sql:        `SELECT Fizz.Buzz FROM Fizz`,
				columnName: "",
			},
			wantErr: true,
		},
		{
			name: "columnName not present",
			args: args{
				sql:        `SELECT Fizz.Buzz FROM Fizz`,
				columnName: "foo",
			},
			wantErr: true,
		},
		{
			name: "syntax error",
			args: args{
				sql:        `SELECT FROM Fizz`,
				columnName: "foo",
			},
			wantErr: true,
		},
		{
			name: "query starts with CTE",
			args: args{
				sql: `WITH f AS (SELECT Id FROM Fizz) SELECT * FROM Buzz JOIN f.Id on Buzz.Fizz`,
			},
			wantErr: true,
		},
		{
			name: "query with joins and aliases",
			args: args{
				sql: `SELECT
	  f.Id,
	  b.Fizz AS FizzBuzz,
	FROM Fizz AS f
	JOIN Buzz AS b ON b.Fizz = f.Id`,
				columnName: "FizzBuzz",
			},
			want: "Buzz",
		},
		{
			name: "query that tries to use all FROM clause syntax",
			args: args{
				sql: `SELECT
	  f.Id,
	  b.Id AS FizzBuzz,
	FROM Fizz AS f
	FULL JOIN UNNEST([0, 1, 2]) AS numbers -- explicit alias
	FULL JOIN UNNEST([3, 4, 5]) -- no alias
	LEFT JOIN Foo ON Foo.Fizzer = f.Id
	JOIN Schema.Bar on Schema.Bar.Fooer = Foo.Id
	JOIN Schema.Bizz AS bizzer on bizzer.Barrer = Schema.Bar.Id
	LEFT JOIN (SELECT Id FROM Bazz) AS b2 ON b2.Id = bizzer.Id
	JOIN (SELECT BazzId FROM Floop) ON BazzId = b2.Id
	FULL JOIN (SELECT 1 as Id) ON Id = f.Id
	JOIN Buzz AS b ON b.Barrer = Bar.Id`,
				columnName: "FizzBuzz",
			},
			want: "Buzz",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := originTableName(tt.args.sql, tt.args.columnName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("originTableName() wantErr=%v err=%v", tt.wantErr, err)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("originTableName() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
