package generation

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/go-playground/errors/v5"
)

func (r *resourceGenerator) generateComputedResourceHandler(res *computedResource) error {
	begin := time.Now()
	fileName := generatedGoFileName(strings.ToLower(caser.ToSnake(res.Name())))
	destinationFilePath := filepath.Join(r.handlerDestination, fileName)

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	tmpl, err := template.New(fmt.Sprintf("computedResourceHandlerTemplate:%q", res.Name())).Funcs(r.templateFuncs()).Parse(computedResourceHandlerTemplate)
	if err != nil {
		return errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, map[string]any{
		"Source":              r.resourcePackageDir,
		"LocalPackageImports": r.localPackageImports(),
		"Resource":            res,
		"Package":             r.handlerDestination,
		"ComputedPackage":     filepath.Base(r.compPackageDir),
		"ApplicationName":     r.applicationName,
		"ReceiverName":        r.receiverName,
	}); err != nil {
		return errors.Wrap(err, "tmpl.Execute()")
	}

	formattedBytes, err := r.GoFormatBytes(file.Name(), buf.Bytes())
	if err != nil {
		return err
	}

	if err := r.WriteBytesToFile(file, formattedBytes); err != nil {
		return err
	}

	log.Printf("Generated RPC handler file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}
