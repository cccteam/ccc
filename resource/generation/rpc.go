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

func (r *resourceGenerator) runRPCGeneration() error {
	if err := removeGeneratedFiles(r.rpc.Dir(), prefix); err != nil {
		return err
	}

	begin := time.Now()

	if err := forEachGo(r.rpcMethods, r.generateRPCMethod); err != nil {
		return err
	}

	if err := r.generateRPCInterfaces(); err != nil {
		return err
	}

	log.Printf("Finished RPC file generation in %s", time.Since(begin))

	return nil
}

func (r *resourceGenerator) generateRPCMethod(rpc *rpcMethodInfo) error {
	fileName := generatedGoFileName(strings.ToLower(caser.ToSnake(rpc.Name())))
	destinationFilePath := filepath.Join(r.rpc.Dir(), fileName)

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	tmpl, err := template.New(fmt.Sprintf("rpcFileTemplate:%q", rpc.Name())).Funcs(r.templateFuncs()).Parse(rpcFileTemplate)
	if err != nil {
		return errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, map[string]any{
		"Source":    r.rpc.Dir(),
		"Package":   r.rpc.Package(),
		"RPCMethod": rpc,
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

	return nil
}

func (r *resourceGenerator) generateRPCHandler(rpcMethod *rpcMethodInfo) error {
	begin := time.Now()
	fileName := generatedGoFileName(strings.ToLower(caser.ToSnake(rpcMethod.Name())))
	destinationFilePath := filepath.Join(r.handler.Dir(), fileName)

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	tmpl, err := template.New(fmt.Sprintf("rcpHandlerTemplate:%q", rpcMethod.Name())).Funcs(r.templateFuncs()).Parse(rpcHandlerTemplate)
	if err != nil {
		return errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, map[string]any{
		"Source":              r.rpc.Dir(),
		"LocalPackageImports": r.localPackageImports(),
		"RPCMethod":           rpcMethod,
		"Package":             r.handler.Package(),
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

func (r *resourceGenerator) generateRPCInterfaces() error {
	output, err := r.generateTemplateOutput("rpcInterfacesTemplate", rpcInterfacesTemplate, map[string]any{
		"Source":  r.rpc.Dir(),
		"Package": r.rpc.Package(),
		"Types":   r.rpcMethods,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join(".", r.rpc.Dir(), generatedGoFileName("rpc_iface"))

	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	output, err = r.GoFormatBytes(file.Name(), output)
	if err != nil {
		return err
	}

	if err := r.WriteBytesToFile(file, output); err != nil {
		return err
	}

	return nil
}
