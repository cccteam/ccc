package generation

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/go-playground/errors/v5"
)

func (r *resourceGenerator) generateRPCHandler(rpcMethod rpcMethodInfo) error {
	fileName := generatedFileName(strings.ToLower(r.caser.ToSnake(rpcMethod.name)))
	destinationFilePath := filepath.Join(r.handlerDestination, fileName)

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	tmpl, err := template.New(fmt.Sprintf("rcpMethodTemplate:%q", rpcMethod.Name())).Funcs(r.templateFuncs()).Parse(rcpMethodTemplate)
	if err != nil {
		return errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, map[string]any{
		"Source":      r.resourceFilePath,
		"PackageName": r.packageName,
		"RPCMethod":   rpcMethod,
	}); err != nil {
		return errors.Wrap(err, "tmpl.Execute()")
	}

	log.Printf("Generating RPC handler file: %s", fileName)

	if err := r.writeBytesToFile(destinationFilePath, file, buf.Bytes(), true); err != nil {
		return err
	}

	return nil
}
