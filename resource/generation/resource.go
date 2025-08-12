package generation

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

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

	// We always want to cache the consolidatedRoute data for the typescript gen
	if c.ConsolidatedRoute != "" {
		if err := cacheData(consolidatedRouteCache, c.consolidateConfig); err != nil {
			return nil, err
		}
	}

	r.client = c

	if err := resolveOptions(r, opts); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *resourceGenerator) Generate(ctx context.Context) error {
	log.Println("Starting ResourceGenerator Generation")

	begin := time.Now()

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

		r.rpcMethods, err = r.structsToRPCMethods(rpcStructs)
		if err != nil {
			return err
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

	log.Printf("Finished Resource generation in %s\n", time.Since(begin))

	return nil
}

func (r *resourceGenerator) runResourcesGeneration() error {
	if err := RemoveGeneratedFiles(r.resourceDestination, Prefix); err != nil {
		return err
	}

	if err := r.generateResourceInterfaces(); err != nil {
		return errors.Wrap(err, "c.generateResourceInterfaces()")
	}

	for i := range r.resources {
		if err := r.generateResources(r.resources[i]); err != nil {
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

func (r *resourceGenerator) generateResources(res resourceInfo) error {
	begin := time.Now()
	fileName := generatedFileName(strings.ToLower(r.caser.ToSnake(r.pluralize(res.Name()))))
	destinationFilePath := filepath.Join(r.resourceDestination, fileName)

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

	log.Printf("Generated resource file in %s: %v\n", time.Since(begin), destinationFilePath)

	return nil
}

func (r *resourceGenerator) generateEnums(namedTypes []*parser.NamedType) error {
	enumMap, err := r.retrieveDatabaseEnumValues(namedTypes)
	if err != nil {
		return err
	}

	output, err := r.generateTemplateOutput("resourceEnumsTemplate", resourceEnumsTemplate, map[string]any{
		"Source":     r.resourceFilePath,
		"NamedTypes": namedTypes,
		"EnumMap":    enumMap,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	file, err := os.Create(filepath.Join(r.resourceDestination, generatedFileName(resourceEnumsFileName)))
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

func (r *resourceGenerator) retrieveDatabaseEnumValues(namedTypes []*parser.NamedType) (map[string][]enumData, error) {
	enumMap := make(map[string][]enumData)
	for _, namedType := range namedTypes {
		scanner := genlang.NewScanner(keywords())
		result, err := scanner.ScanNamedType(namedType)
		if err != nil {
			return nil, errors.Wrap(err, "scanner.ScanNamedType()")
		}

		var tableName string
		if result.Named.Has(enumerateKeyword) {
			tableName = result.Named.GetOne(enumerateKeyword).Arg1
		} else {
			continue
		}

		if t := namedType.TypeInfo.TypeName(); t != "string" {
			return nil, errors.Newf("cannot enumerate type %q, underlying type must be %q, found %q", namedType.Name(), "string", t)
		}

		data, ok := r.enumValues[tableName]
		if !ok {
			return nil, errors.Newf("cannot enumerate type %q, tableName %q has no values or does not exist", namedType.Name(), tableName)
		}

		enumMap[namedType.Name()] = data
	}

	return enumMap, nil
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
