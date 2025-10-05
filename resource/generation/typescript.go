package generation

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
)

type typescriptGenerator struct {
	*client
	genPermission          bool
	genMetadata            bool
	genEnums               bool
	typescriptDestination  string
	typescriptOverrides    map[string]string
	rc                     *resource.Collection
	routerResources        []accesstypes.Resource
	spannerEmulatorVersion string
}

// NewTypescriptGenerator constructs a new Generator for generating Typescript for a resource-driven Angular app.
func NewTypescriptGenerator(ctx context.Context, resourceSourcePath, migrationSourceURL, targetDir string, rc *resource.Collection, options ...TSOption) (Generator, error) {
	if rc == nil {
		return nil, errors.New("resource collection cannot be nil")
	}

	t := &typescriptGenerator{
		rc:                    rc,
		routerResources:       rc.Resources(),
		typescriptDestination: targetDir,
	}

	opts := make([]option, 0, len(options))
	for _, opt := range options {
		opts = append(opts, opt)
	}

	c, err := newClient(ctx, typeScriptGeneratorType, resourceSourcePath, migrationSourceURL, nil, opts)
	if err != nil {
		return nil, err
	}

	t.client = c

	if err := resolveOptions(t, opts); err != nil {
		return nil, err
	}

	return t, nil
}

func (t *typescriptGenerator) Generate() error {
	log.Println("Starting TypescriptGenerator Generation")

	begin := time.Now()

	packageMap, err := parser.LoadPackages(t.loadPackages...)
	if err != nil {
		return errors.Wrap(err, "parser.LoadPackages()")
	}

	resourcesPkg := parser.ParsePackage(packageMap["resources"])

	resources, err := t.extractResources(resourcesPkg.Structs)
	if err != nil {
		return err
	}

	t.resources = make([]*resourceInfo, 0, len(resources))
	for _, res := range resources {
		if t.rc.ResourceExists(accesstypes.Resource(t.pluralize(res.Name()))) {
			res.Fields = t.resourceFieldsTypescriptType(res.Fields)
			t.resources = append(t.resources, res)
		}
	}

	if t.genRPCMethods {
		rpcStructs := parser.ParsePackage(packageMap["rpc"]).Structs

		rpcStructs = parser.FilterStructsByInterface(rpcStructs, rpcInterfaces[:])

		t.rpcMethods, err = t.structsToRPCMethods(rpcStructs)
		if err != nil {
			return err
		}

		for _, rpcMethod := range t.rpcMethods {
			rpcMethod.Fields = t.rpcFieldsTypescriptType(rpcMethod.Fields)
		}
	}

	if t.genMetadata {
		if err := t.runTypescriptMetadataGeneration(); err != nil {
			return err
		}
	}
	if t.genPermission {
		if err := t.runTypescriptPermissionGeneration(); err != nil {
			return err
		}
	}
	if t.genEnums {
		if err := t.runTypescriptEnumGeneration(resourcesPkg.NamedTypes); err != nil {
			return err
		}
	}

	log.Printf("Finished Typescript generation in %s\n", time.Since(begin))

	return nil
}

func (t *typescriptGenerator) runTypescriptEnumGeneration(namedTypes []*parser.NamedType) error {
	if !t.genMetadata && !t.genPermission {
		if err := removeGeneratedFiles(t.typescriptDestination, headerComment); err != nil {
			return errors.Wrap(err, "RemoveGeneratedFiles()")
		}
	}

	if err := t.generateEnums(namedTypes); err != nil {
		return errors.Wrap(err, "generateEnums")
	}

	return nil
}

func (t *typescriptGenerator) runTypescriptPermissionGeneration() error {
	begin := time.Now()
	if !t.genMetadata {
		if err := removeGeneratedFiles(t.typescriptDestination, headerComment); err != nil {
			return errors.Wrap(err, "RemoveGeneratedFiles()")
		}
	}

	log.Println("Starting typescript resource permission generation...")

	routerData := t.rc.TypescriptData()

	templateData := map[string]any{
		"File":       t,
		"Data":       routerData,
		"RPCMethods": t.rpcMethods,
	}

	output, err := t.generateTemplateOutput(typescriptConstantsTemplate, typescriptConstantsTemplate, templateData)
	if err != nil {
		return errors.Wrap(err, "c.generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, generatedTypescriptFileName("constants"))
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated Permissions in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

func (t *typescriptGenerator) runTypescriptMetadataGeneration() error {
	if err := removeGeneratedFiles(t.typescriptDestination, headerComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	if err := t.generateTypescriptMetadata(); err != nil {
		return errors.Wrap(err, "generateTypescriptResources")
	}

	return nil
}

func (t *typescriptGenerator) generateTypescriptMetadata() error {
	begin := time.Now()
	log.Println("Starting typescript metadata generation...")

	if err := t.generateResourceMetadata(); err != nil {
		return errors.Wrap(err, "generateResourceMetadata()")
	}

	if err := t.generateMethodMetadata(); err != nil {
		return errors.Wrap(err, "generateMethodMetadata()")
	}

	log.Printf("Generated typescript metadata in %s\n", time.Since(begin))

	return nil
}

func (t *typescriptGenerator) generateResourceMetadata() error {
	begin := time.Now()
	log.Println("Starting resource metadata generation...")
	output, err := t.generateTemplateOutput(typescriptResourcesTemplate, typescriptResourcesTemplate, map[string]any{
		"File":              t,
		"Resources":         t.resources,
		"ConsolidatedRoute": t.ConsolidatedRoute,
		"GenPrefix":         genPrefix,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, generatedTypescriptFileName("resources"))
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated resource metadata in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

func (t *typescriptGenerator) generateMethodMetadata() error {
	begin := time.Now()
	log.Println("Starting method metadata generation...")

	output, err := t.generateTemplateOutput(typescriptMethodsTemplate, typescriptMethodsTemplate, map[string]any{
		"File":       t,
		"RPCMethods": t.rpcMethods,
		"GenPrefix":  genPrefix,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, generatedTypescriptFileName("methods"))
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated methods metadata in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

func (t *typescriptGenerator) generateEnums(namedTypes []*parser.NamedType) error {
	begin := time.Now()
	log.Println("Starting enum generation...")

	enumMap, err := t.retrieveDatabaseEnumValues(namedTypes)
	if err != nil {
		return err
	}

	output, err := t.generateTemplateOutput("typescriptEnumsTemplate", typescriptEnumsTemplate, map[string]any{
		"Source":     t.resourceFilePath,
		"NamedTypes": namedTypes,
		"EnumMap":    enumMap,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	file, err := os.Create(filepath.Join(t.typescriptDestination, generatedTypescriptFileName("enums")))
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated enums in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

func (t *typescriptGenerator) resourceFieldsTypescriptType(fields []*resourceField) []*resourceField {
	for _, field := range fields {
		if override, ok := t.typescriptOverrides[field.TypeName()]; ok {
			field.typescriptType = override
		} else {
			field.typescriptType = stringGoType
		}

		if field.IsIterable() {
			field.typescriptType += "[]"
		}

		if field.IsForeignKey && slices.Contains(t.routerResources, accesstypes.Resource(field.ReferencedResource)) {
			field.IsEnumerated = true
		}
	}

	return fields
}

func (t *typescriptGenerator) rpcFieldsTypescriptType(fields []*rpcField) []*rpcField {
	for _, field := range fields {
		if override, ok := t.typescriptOverrides[field.TypeName()]; ok {
			if override == booleanStr && field.Type() == "*bool" {
				panic("Bool pointer (*bool) not currently supported for rpc methods.")
			}
			field.typescriptType = override
		} else {
			field.typescriptType = stringGoType
		}

		if field.IsIterable() {
			field.typescriptType += "[]"
		}
	}

	return fields
}
