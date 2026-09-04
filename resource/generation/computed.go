package generation

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

func (r *resourceGenerator) generateComputedResourceHandler(res *computedResource) error {
	begin := time.Now()
	// The handler file is plural like every other resource kind's (and like the
	// handler method it holds); the singular stem also risked ending in _test.go.
	fileName := generatedGoFileName(strings.ToLower(caser.ToSnake(r.pluralize(res.Name()))))
	destinationFilePath := filepath.Join(r.handler.Dir(), fileName)

	if err := r.writeFormattedGoFile(destinationFilePath, fmt.Sprintf("computedResourceHandlerTemplate:%q", res.Name()), computedResourceHandlerTemplate, &computedHandlerData{
		Source:              r.computed.Dir(),
		LocalPackageImports: r.localPackageImports(),
		Resource:            res,
		Package:             r.handler.Package(),
		ComputedPackage:     r.computed.Package(),
		ApplicationName:     r.applicationName,
		ReceiverName:        r.receiverName,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}

	log.Printf("Generated computed resource handler file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}
