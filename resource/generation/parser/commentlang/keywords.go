package commentlang

import "github.com/go-playground/errors/v5"

type (
	keyword     string
	keywordOpts struct {
		argsRequired           bool
		noArgs                 bool
		argsRequiredWhenStruct bool
		argsRequiredWhenField  bool
		noArgsWhenStruct       bool
		noArgsWhenField        bool
		exclusive              bool
		scanArgs               func(s *scanner) (KeywordArguments, error)
	}
	Keyword interface{ isKeyword() }
)

func (keyword) isKeyword() {}

var keywords = map[keyword]keywordOpts{
	illegal:     {},
	PrimaryKey:  {argsRequiredWhenStruct: true, noArgsWhenField: true, exclusive: true, scanArgs: scanSingleArg},
	ForeignKey:  {argsRequired: true, scanArgs: scanDualArgs},
	Check:       {argsRequired: true, scanArgs: scanSingleArg},
	Default:     {argsRequired: true, scanArgs: scanSingleArg},
	Substring:   {argsRequired: true, scanArgs: scanSingleArg},
	UniqueIndex: {argsRequiredWhenStruct: true, noArgsWhenField: true, scanArgs: scanSingleArg},
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

func scanSingleArg(s *scanner) (KeywordArguments, error) {
	args, err := s.scanArguments()
	if err != nil {
		return nil, err
	}

	return singleArg{arg: string(args)}, nil
}

type dualArgs struct {
	arg1 string
	arg2 string
}

func (f dualArgs) Arguments() []string {
	return []string{f.arg1, f.arg2}
}

func scanDualArgs(s *scanner) (KeywordArguments, error) {
	arg1, err := s.scanArguments()
	if err != nil {
		return nil, err
	}

	if peek, ok := s.peekNext(); !ok || peek != byte('(') {
		return nil, errors.New(s.error("expected second argument"))
	}

	arg2, err := s.scanArguments()
	if err != nil {
		return nil, err
	}

	return dualArgs{string(arg1), string(arg2)}, nil
}
