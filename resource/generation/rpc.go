package generation

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/go-playground/errors/v5"
)

func (r *resourceGenerator) runRPCGeneration() error {
	if err := RemoveGeneratedFiles(r.rpcPackageDir, Prefix); err != nil {
		return err
	}
	if err := RemoveGeneratedFiles(r.businessLayerPackageDir, Prefix); err != nil {
		return err
	}

	if err := r.generateRPCInterfaces(); err != nil {
		return err
	}

	if err := r.generateBusinessLayerInterfaces(); err != nil {
		return err
	}

	var (
		wg sync.WaitGroup

		errChan = make(chan error)
	)
	for _, rpcMethod := range r.rpcMethods {
		wg.Add(1)
		go func(rpcMethod *rpcMethodInfo) {
			if err := r.generateRPCMethod(rpcMethod); err != nil {
				errChan <- err
			}
			wg.Done()
		}(rpcMethod)
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	var rpcMethodErrors error
	for e := range errChan {
		rpcMethodErrors = errors.Join(rpcMethodErrors, e)
	}

	if rpcMethodErrors != nil {
		return rpcMethodErrors
	}

	return nil
}

func (r *resourceGenerator) generateRPCHandler(rpcMethod *rpcMethodInfo) error {
	fileName := generatedFileName(strings.ToLower(r.caser.ToSnake(rpcMethod.Name())))
	destinationFilePath := filepath.Join(r.handlerDestination, fileName)

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
		"Source":              r.resourceFilePath,
		"LocalPackageImports": r.localPackageImports(),
		"RPCMethod":           rpcMethod,
	}); err != nil {
		return errors.Wrap(err, "tmpl.Execute()")
	}

	log.Printf("Generating RPC handler file: %s", fileName)

	formattedBytes, err := r.GoFormatBytes(file.Name(), buf.Bytes())
	if err != nil {
		return err
	}

	if err := r.WriteBytesToFile(file, formattedBytes); err != nil {
		return err
	}

	return nil
}

func (r *resourceGenerator) generateRPCMethod(rpcMethod *rpcMethodInfo) error {
	fileName := generatedFileName(strings.ToLower(r.caser.ToSnake(rpcMethod.Name())))
	destinationFilePath := filepath.Join(r.businessLayerPackageDir, fileName)

	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	tmpl, err := template.New(fmt.Sprintf("rcpMethodTemplate:%q", rpcMethod.Name())).Funcs(r.templateFuncs()).Parse(rpcMethodTemplate)
	if err != nil {
		return errors.Wrap(err, "template.New().Parse()")
	}

	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, map[string]any{
		"Source":              r.resourceFilePath,
		"LocalPackageImports": r.localPackageImports(),
		"RPCMethod":           rpcMethod,
	}); err != nil {
		return errors.Wrap(err, "tmpl.Execute()")
	}

	log.Printf("Generating RPC handler file: %s", fileName)

	formattedBytes, err := r.GoFormatBytes(file.Name(), buf.Bytes())
	if err != nil {
		return err
	}

	if err := r.WriteBytesToFile(file, formattedBytes); err != nil {
		return err
	}

	return nil
}

func (r *resourceGenerator) generateRPCInterfaces() error {
	output, err := r.generateTemplateOutput("rpcInterfacesTemplate", rpcInterfacesTemplate, map[string]any{
		"Source": r.resourceFilePath,
		"Types":  r.rpcMethods,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join("./businesslayer/rpc", generatedFileName("rpc_iface"))

	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	formattedBytes, err := r.GoFormatBytes(file.Name(), output)
	if err != nil {
		return err
	}

	if err := r.WriteBytesToFile(file, formattedBytes); err != nil {
		return err
	}

	return nil
}

func (r *resourceGenerator) generateBusinessLayerInterfaces() error {
	output, err := r.generateTemplateOutput("businessLayerInterfaces", businesslayerInterfacesTemplate, map[string]any{
		"Source":              r.resourceFilePath,
		"LocalPackageImports": r.localPackageImports(),
		"RPCMethods":          r.rpcMethods,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join("./businesslayer", generatedFileName("iface"))

	file, err := os.Create(destinationFile)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	formattedBytes, err := r.GoFormatBytes(file.Name(), output)
	if err != nil {
		return err
	}

	if err := r.WriteBytesToFile(file, formattedBytes); err != nil {
		return err
	}

	return nil
}
