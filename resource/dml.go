package resource

type dbType string

const (
	spannerdbType  dbType = "spanner"
	postgresdbType dbType = "postgres"
)

type (
	Columns string
	Where   string
	Stmt    string
)
