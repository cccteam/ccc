package generation

var baseTypes = []string{
	"bool",
	"string",
	"int", "int8", "int16", "int32", "int64",
	"float32", "float64",
	"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	"byte",
	"rune",
	"complex64", "complex128",
	"error",
}

const routeGenerationDeclaration = "generation:routes"

type ConstraintType string

const (
	PrimaryKey ConstraintType = "PRIMARY KEY"
	ForeignKey ConstraintType = "FOREIGN KEY"
)

type HandlerType string

const (
	List  HandlerType = "list"
	Read  HandlerType = "read"
	Patch HandlerType = "patch"
)

type OptionType string

const (
	Regenerate OptionType = "regenerate"
	NoGenerate OptionType = "nogenerate"
)

type PatchType string

const (
	CreatePatch PatchType = "Create"
	UpdatePatch PatchType = "Update"
)

const (
	querySetOutputFilename          = "types.go"
	resourceInterfaceOutputFilename = "resources_iface.go"
	resourcesTestFilename           = "generated_resources_test.go"
	routesFilename                  = "generated_routes.go"
)

type Config struct {
	ResourceSource           string
	SpannerDestination       string
	HandlerDestination       string
	RoutesDestination        string
	RoutesDestinationPackage string
	RoutePrefix              string
	Migrations               string
	PluralOverrides          map[string]string
	CaserGoInitialisms       map[string]bool
	IgnoredHandlers          map[string][]HandlerType
}

type generatedType struct {
	Name            string
	IsView          bool
	IsCompoundTable bool
	Fields          []*typeField
}

type typeField struct {
	Name           string
	Type           string
	Tag            string
	IsPrimaryKey   bool
	IsIndex        bool
	QueryTag       string
	ConstraintType string
	ReadPerm       string
	ListPerm       string
	PatchPerm      string
	Conditions     []string
}

type FieldMetadata struct {
	ConstraintType ConstraintType
	ColumnName     string
	SpannerType    string
	IsNullable     bool
	IsIndex        bool
}

type InformationSchemaResult struct {
	TableName       string  `spanner:"TABLE_NAME"`
	ColumnName      string  `spanner:"COLUMN_NAME"`
	ConstraintName  *string `spanner:"CONSTRAINT_NAME"`
	ConstraintType  *string `spanner:"CONSTRAINT_TYPE"`
	SpannerType     string  `spanner:"SPANNER_TYPE"`
	IsNullable      bool    `spanner:"IS_NULLABLE"`
	IsView          bool    `spanner:"IS_VIEW"`
	IsIndex         bool    `spanner:"IS_INDEX"`
	OrdinalPosition int64   `spanner:"ORDINAL_POSITION"`
}

type TableMetadata struct {
	Columns map[string]FieldMetadata
	IsView  bool
}

type generationOption struct {
	option  OptionType
	handler HandlerType
}

type generatedHandler struct {
	template    string
	handlerType HandlerType
}

type generatedRoute struct {
	Method      string
	Path        string
	HandlerFunc string
}
