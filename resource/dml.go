package resource

type DBType string

const (
	SpannerDBType  DBType = "spanner"
	PostgresDBType DBType = "postgres"
)

type Statement struct {
	Sql    string
	Params map[string]any
}

type Columns string

type Config struct {
	DBType              DBType
	ChangeTrackingTable string
	TrackChanges        bool
}

func (c Config) SetDBType(dbType DBType) Config {
	c.DBType = dbType

	return c
}

func (c Config) SetChangeTrackingTable(changeTrackingTable string) Config {
	c.ChangeTrackingTable = changeTrackingTable

	return c
}

func (c Config) SetTrackChanges(trackChanges bool) Config {
	c.TrackChanges = trackChanges

	return c
}

type Configurer interface {
	Config() Config
}
