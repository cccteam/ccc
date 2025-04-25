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
	}
)

func (keyword) isKeyword() {}

var keywords = map[keyword]keywordOpts{
	illegal:     {},
	PrimaryKey:  {ScanStruct: argsRequired | exclusive, ScanField: noArgs | exclusive},
	ForeignKey:  {ScanStruct: dualArgsRequired, ScanField: argsRequired | exclusive},
	Check:       {ScanStruct: argsRequired, ScanField: argsRequired},
	Default:     {ScanField: argsRequired | exclusive},
	Substring:   {ScanField: argsRequired | exclusive},
	UniqueIndex: {ScanStruct: argsRequired, ScanField: noArgs},
	Query:       {ScanStruct: argsRequired | exclusive},
	As:          {ScanField: argsRequired | exclusive},
}

const (
	// remember to add new keywords to the map above ^^^
	illegal     keyword = ""
	PrimaryKey  keyword = "primarykey"
	ForeignKey  keyword = "foreignkey"
	Check       keyword = "check"
	Default     keyword = "default"
	Substring   keyword = "substring"
	UniqueIndex keyword = "uniqueindex"
	Query       keyword = "query"
	As          keyword = "as"
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
