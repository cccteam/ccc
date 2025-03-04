// lint is a custom linter that checks if otel span names match function names.
package lint

import (
	"github.com/cccteam/ccc/lint/errwrap"
	"github.com/cccteam/ccc/lint/otelspanname"
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("ccclint", New)
}

type CCCLint struct {
	settings Settings
}

type Settings struct {
	// We can add settings/configs here
}

func New(settings any) (register.LinterPlugin, error) {
	// The configuration type will be map[string]any or []interface, it depends on your configuration.
	// You can use https://github.com/go-viper/mapstructure to convert map to struct.

	s, err := register.DecodeSettings[Settings](settings)
	if err != nil {
		return nil, err
	}

	return &CCCLint{settings: s}, nil
}

func (c *CCCLint) BuildAnalyzers() ([]*analysis.Analyzer, error) {
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

func (c *CCCLint) GetLoadMode() string {
	return register.LoadModeSyntax
}
