package commentlang

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

var keywords = map[keyword]keywordOpts{
	illegal:     {},
	PrimaryKey:  {ScanStruct: argsRequired | exclusive, ScanField: noArgs | exclusive},
	ForeignKey:  {ScanStruct: dualArgsRequired, ScanField: argsRequired},
	Check:       {ScanField: argsRequired | exclusive},
	Default:     {ScanField: argsRequired | exclusive},
	Hidden:      {ScanField: noArgs | exclusive},
	Substring:   {ScanField: argsRequired},
	Fulltext:    {ScanField: argsRequired},
	Ngram:       {ScanField: argsRequired},
	UniqueIndex: {ScanStruct: argsRequired, ScanField: noArgs},
	View:        {ScanStruct: noArgs | exclusive},
	Query:       {ScanStruct: argsRequired | exclusive},
	Using:       {ScanField: argsRequired | exclusive},
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
)

type KeywordArguments interface {
	Arguments() []string
}

type singleArg struct {
	arg string
}

func (d singleArg) Arguments() []string {
	return []string{d.arg}
}

type dualArgs struct {
	arg1 string
	arg2 string
}

func (f dualArgs) Arguments() []string {
	return []string{f.arg1, f.arg2}
}
