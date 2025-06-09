package genlang

type keywordFlag int

const (
	prohibited   keywordFlag = 0
	argsRequired keywordFlag = 1 << iota
	dualArgsRequired
	noArgs
	exclusive // limit instance of the keyword to 1 per field or struct
)

type (
	keyword     string
	keywordOpts map[scanMode]keywordFlag
	Keyword     interface {
		isKeyword()
		String() string
	}
)

func (keyword) isKeyword() {}
func (k keyword) String() string {
	return string(k)
}

// TODO(jrowland): consider taking this map as an argument to this package,
// making keywords programmable for any use case
var keywords = map[keyword]keywordOpts{
	illegal:     {},
	PrimaryKey:  {scanStruct: argsRequired | exclusive, scanField: noArgs | exclusive},
	ForeignKey:  {scanStruct: dualArgsRequired, scanField: argsRequired},
	Check:       {scanField: argsRequired | exclusive},
	Default:     {scanField: argsRequired | exclusive},
	Hidden:      {scanField: noArgs | exclusive},
	Substring:   {scanField: argsRequired},
	Fulltext:    {scanField: argsRequired},
	Ngram:       {scanField: argsRequired},
	UniqueIndex: {scanStruct: argsRequired, scanField: noArgs},
	View:        {scanStruct: noArgs | exclusive},
	Query:       {scanStruct: argsRequired | exclusive},
	Using:       {scanField: argsRequired | exclusive},
	Suppress:    {scanField: noArgs | exclusive},
}

const (
	// remember to add new keywords to the map above ^^^
	illegal     keyword = ""
	PrimaryKey  keyword = "primarykey"
	ForeignKey  keyword = "foreignkey"
	Check       keyword = "check"
	Default     keyword = "default"
	Hidden      keyword = "hidden"
	Substring   keyword = "substring"
	Fulltext    keyword = "fulltext"
	Ngram       keyword = "ngram"
	UniqueIndex keyword = "uniqueindex"
	View        keyword = "view"  // Designates a struct as a view
	Query       keyword = "query" // The query to be used for a view. Required if @view is used.
	Using       keyword = "using" // Can only be used in views. Names the source field from another struct if it does not match this field
	Suppress    keyword = "suppress"
)

// TODO(jrowland): find a better way to handle single vs dual arg results
type Args struct {
	Arg1 string
	Arg2 *string
}
