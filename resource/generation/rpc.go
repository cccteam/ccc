package generation

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/go-playground/errors/v5"
)

func (r *resourceGenerator) runRPCGeneration() error {
	r.output.registerOutput(r.rpc.Dir(), prefix)

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
	fileName := generatedGoFileName(fileStem(rpc.Name()))
	destinationFilePath := filepath.Join(r.rpc.Dir(), fileName)

	if err := r.writeFormattedGoFile(destinationFilePath, fmt.Sprintf("rpcFileTemplate:%q", rpc.Name()), rpcFileTemplate, &rpcFileData{
		Source:    r.rpc.Dir(),
		Package:   r.rpc.Package(),
		RPCMethod: rpc,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}

	return nil
}

func (r *resourceGenerator) generateRPCHandler(rpcMethod *rpcMethodInfo) error {
	begin := time.Now()
	fileName := generatedGoFileName(fileStem(rpcMethod.Name()))
	destinationFilePath := filepath.Join(r.handler.Dir(), fileName)

	template := rpcHandlerTemplate
	if rpcMethod.Upload != nil {
		// The multipart intake is its own frame.
		template = rpcUploadHandlerTemplate
	}

	if err := r.writeFormattedGoFile(destinationFilePath, fmt.Sprintf("rcpHandlerTemplate:%q", rpcMethod.Name()), template, &rpcHandlerData{
		Source:              r.rpc.Dir(),
		LocalPackageImports: r.localPackageImports(),
		RPCMethod:           rpcMethod,
		Package:             r.handler.Package(),
		ApplicationName:     r.applicationName,
		ReceiverName:        r.receiverName,
		ResourcesPackage:    r.resource.Package(),
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}

	log.Printf("Generated RPC handler file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

// generateScheduledHandler writes a scheduled method's handler beside the RPC handlers:
// the frame Cloud Scheduler's verified call runs, with no session, no permission check
// and no request body.
func (r *resourceGenerator) generateScheduledHandler(method *rpcMethodInfo) error {
	begin := time.Now()
	destinationFilePath := filepath.Join(r.handler.Dir(), generatedGoFileName(fileStem(method.Name())))

	if err := r.writeFormattedGoFile(destinationFilePath, fmt.Sprintf("scheduledHandlerTemplate:%q", method.Name()), scheduledHandlerTemplate, &rpcHandlerData{
		Source:              r.rpc.Dir(),
		LocalPackageImports: r.localPackageImports(),
		RPCMethod:           method,
		Package:             r.handler.Package(),
		ApplicationName:     r.applicationName,
		ReceiverName:        r.receiverName,
		ResourcesPackage:    r.resource.Package(),
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}

	log.Printf("Generated scheduled handler file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}

func (r *resourceGenerator) generateRPCInterfaces() error {
	destinationFile := filepath.Join(".", r.rpc.Dir(), generatedGoFileName("rpc_iface"))

	if err := r.writeFormattedGoFile(destinationFile, "rpcInterfacesTemplate", rpcInterfacesTemplate, &rpcInterfacesData{
		Source:  r.rpc.Dir(),
		Package: r.rpc.Package(),
		Types:   r.rpcMethods,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}

	return nil
}
