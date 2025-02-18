// ccc-lint is a custom linter that checks if otel span names match function names.
package main

import (
	"github.com/cccteam/ccc/lint/errwrap"
	"github.com/cccteam/ccc/lint/otelspanname"
	"golang.org/x/tools/go/analysis"
)

//go:generate go build -buildmode=plugin -o ccc-lint.so lint.go

func New(conf any) ([]*analysis.Analyzer, error) {
	var analyzers []*analysis.Analyzer

	otelspannameAnalyzer, err := otelspanname.New()
	if err != nil {
		return nil, err
	}
	analyzers = append(analyzers, otelspannameAnalyzer)

	errwrapAnalyzer, err := errwrap.New()
	if err != nil {
		return nil, err
	}
	analyzers = append(analyzers, errwrapAnalyzer)

	return analyzers, nil
}
