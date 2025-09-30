// Package lint is a custom linter for custom ccc lint rules.
package lint

import (
	"github.com/cccteam/ccc/lint/errwrap"
	"github.com/cccteam/ccc/lint/otelspanname"
	"github.com/go-playground/errors/v5"
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("ccclint", New)
}

// CCCLint is the main struct for the ccc/lint plugin.
type CCCLint struct {
	settings Settings
}

// Settings holds the configuration for the CCCLint linter.
type Settings struct {
	// We can add settings/configs here
}

// New creates a new instance of the CCCLint plugin.
func New(settings any) (register.LinterPlugin, error) {
	// The configuration type will be map[string]any or []interface, it depends on your configuration.
	// You can use https://github.com/go-viper/mapstructure to convert map to struct.

	s, err := register.DecodeSettings[Settings](settings)
	if err != nil {
		return nil, err
	}

	return &CCCLint{settings: s}, nil
}

// BuildAnalyzers builds the analyzers for the CCCLint plugin.
func (c *CCCLint) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	var analyzers []*analysis.Analyzer

	otelspannameAnalyzer, err := otelspanname.New()
	if err != nil {
		return nil, errors.Wrap(err, "otelspanname.New()")
	}
	analyzers = append(analyzers, otelspannameAnalyzer)

	errwrapAnalyzer, err := errwrap.New()
	if err != nil {
		return nil, errors.Wrap(err, "errwrap.New()")
	}
	analyzers = append(analyzers, errwrapAnalyzer)

	return analyzers, nil
}

// GetLoadMode returns the load mode for the CCCLint plugin.
func (c *CCCLint) GetLoadMode() string {
	return register.LoadModeSyntax
}
