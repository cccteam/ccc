package sqlbuilder_test

import (
	"strings"
	"testing"

	sb "github.com/cccteam/ccc/sqlbuilder"
)

func TestSelect(t *testing.T) {
	tests := []struct {
		name    string
		stmts   []sb.Statement
		wantSql string
		wantErr bool
	}{
		{
			name:    "error due to missing required COLUMNS statement",
			wantErr: true,
		},
		{
			name: "multiple columns",
			stmts: []sb.Statement{
				sb.Columns(
					sb.Identifier("apple"),
					sb.Identifier("pear"),
				),
			},
			wantSql: "SELECT\n\tapple, pear",
		},
		{
			name: "from",
			stmts: []sb.Statement{
				sb.Columns(
					sb.Identifier("apple"),
					sb.Identifier("pear"),
				),
				sb.Identifier("fruits"),
			},
			wantSql: "SELECT\n\tapple, pear\nfruits",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stmt := sb.Select(tt.stmts...)
			if err := stmt.Error(); err != nil {
				if !tt.wantErr {
					t.Fatal("unexpected error")
				}

				return
			}

			b := &strings.Builder{}
			n, err := stmt.WriteSql(b)
			t.Logf("bytes written: %v", n)
			if err != nil {
				t.Log(err)
				if !tt.wantErr {
					t.Fatal("unexpected error")
				}
			}

			if gotSql := b.String(); gotSql != tt.wantSql {
				t.Fatalf("sql did not match\nwant: \n%s \ngot = \n%s", tt.wantSql, gotSql)
			}
		})
	}
}
