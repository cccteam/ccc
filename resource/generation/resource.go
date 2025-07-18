package generation

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

type resourceGenerator struct {
	*client
	genHandlers             bool
	genRoutes               bool
	resourceDestination     string
	handlerDestination      string
	routerDestination       string
	routerPackage           string
	routePrefix             string
	rpcPackageDir           string
	businessLayerPackageDir string
}

func NewResourceGenerator(ctx context.Context, resourceSourcePath, migrationSourceURL string, localPackages []string, options ...ResourceOption) (Generator, error) {
	r := &resourceGenerator{
		resourceDestination: filepath.Dir(resourceSourcePath),
	}

	opts := make([]option, 0, len(options))
	for _, opt := range options {
		opts = append(opts, opt)
	}

	c, err := newClient(ctx, resourceSourcePath, migrationSourceURL, localPackages, opts)
	if err != nil {
		return nil, err
	}

	r.client = c

	if err := resolveOptions(r, opts); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *resourceGenerator) Generate() error {
	log.Println("Starting ResourceGenerator Generation")

	packageMap, err := parser.LoadPackages(r.loadPackages...)
	if err != nil {
		return err
	}

	resourcesPkg := parser.ParsePackage(packageMap["resources"])

	resources, err := r.extractResources(resourcesPkg.Structs)
	if err != nil {
		return err
	}

	r.resources = resources

	if err := r.runResourcesGeneration(); err != nil {
		return err
	}

	if err := r.generateEnums(resourcesPkg.NamedTypes); err != nil {
		return err
	}

	if r.genRPCMethods {
		rpcStructs := parser.ParsePackage(packageMap["rpc"]).Structs

		rpcStructs = parser.FilterStructsByInterface(rpcStructs, rpcInterfaces[:])

		r.rpcMethods = nil
		for _, s := range rpcStructs {
			methodInfo, err := r.structToRPCMethod(s)
			if err != nil {
				return err
			}
			r.rpcMethods = append(r.rpcMethods, methodInfo)
		}

		if err := r.runRPCGeneration(); err != nil {
			return err
		}
	}

	if r.genRoutes {
		if err := r.runRouteGeneration(); err != nil {
			return err
		}
	}
	if r.genHandlers {
		if err := r.runHandlerGeneration(); err != nil {
			return err
		}
	}

	return nil
}

func (r *resourceGenerator) runResourcesGeneration() error {
	if err := removeGeneratedFiles(r.resourceDestination, Prefix); err != nil {
		return err
	}

	if err := r.generateResourceInterfaces(); err != nil {
		return errors.Wrap(err, "c.generateResourceInterfaces()")
	}

	for _, resource := range r.resources {
		if err := r.generateResources(resource); err != nil {
			return errors.Wrap(err, "c.generateResources()")
		}
	}

	if err := r.generateResourceTests(); err != nil {
		return errors.Wrap(err, "c.generateResourceTests()")
	}

	return nil
}

func (r *resourceGenerator) generateResourceInterfaces() error {
	output, err := r.generateTemplateOutput("resourcesInterfaceTemplate", resourcesInterfaceTemplate, map[string]any{
		"Source": r.resourceFilePath,
		"Types":  r.resources,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join(r.resourceDestination, generatedFileName(resourceInterfaceOutputName))

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

func (r *resourceGenerator) generateResourceTests() error {
	output, err := r.generateTemplateOutput("resourcesTestTemplate", resourcesTestTemplate, map[string]any{
		"Source":    r.resourceFilePath,
		"Resources": r.resources,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFile := filepath.Join(r.resourceDestination, resourcesTestFileName)

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

func (r *resourceGenerator) generateResources(res *resourceInfo) error {
	fileName := generatedFileName(strings.ToLower(r.caser.ToSnake(r.pluralize(res.Name()))))
	destinationFilePath := filepath.Join(r.resourceDestination, fileName)

	log.Printf("Generating resource file: %v\n", fileName)

	output, err := r.generateTemplateOutput("resourceFileTemplate", resourceFileTemplate, map[string]any{
		"Source":   r.resourceFilePath,
		"Resource": res,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	file, err := os.Create(destinationFilePath)
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

func (r *resourceGenerator) generateEnums(namedTypes []*parser.NamedType) error {
	scanner := genlang.NewScanner(keywords())

	for _, namedType := range namedTypes {
		result, err := scanner.ScanNamedType(namedType)
		if err != nil {
			return errors.Wrap(err, "scanner.ScanNamedType()")
		}

		var resourceName string
		if result.Named.Has("enumerate") {
			resourceName = result.Named.Get("enumerate")[0].Arg1
		} else {
			continue
		}

		if ok := r.doesResourceExist(resourceName); !ok {
			return errors.Newf("cannot enumerate type %q because resource %q does not exist", namedType.Name(), resourceName)
		}

		// TODO: gather enumData values for resourceName

		// TODO: generate constants like
		// const IDValueEnumName TypeName = DescriptionValue

	}

	return nil
}

func (r *resourceGenerator) generateTemplateOutput(templateName, fileTemplate string, data map[string]any) ([]byte, error) {
	tmpl, err := template.New(templateName).Funcs(r.templateFuncs()).Parse(fileTemplate)
	if err != nil {
		return nil, errors.Wrap(err, "template.Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, data); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}

func (r *resourceGenerator) doesResourceExist(resourceName string) bool {
	for i := range r.resources {
		if r.pluralize(r.resources[i].Name()) == resourceName {
			return true
		}
	}

	return false
}
