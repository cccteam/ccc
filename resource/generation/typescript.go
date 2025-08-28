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

func NewTypescriptGenerator(ctx context.Context, resourceSourcePath, migrationSourceURL string, targetDir string, rc *resource.Collection, options ...TSOption) (Generator, error) {
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

func (t *typescriptGenerator) Generate(ctx context.Context) error {
	log.Println("Starting TypescriptGenerator Generation")

	begin := time.Now()

	packageMap, err := parser.LoadPackages(t.loadPackages...)
	if err != nil {
		return err
	}

	resourcesPkg := parser.ParsePackage(packageMap["resources"])

	resources, err := t.extractResources(resourcesPkg.Structs)
	if err != nil {
		return err
	}

	t.resources = make([]resourceInfo, 0, len(resources))
	for i := range resources {
		resource := accesstypes.Resource(t.pluralize(resources[i].Name()))
		if t.rc.ResourceExists(resource) {
			resources[i].Fields = t.resourceFieldsTypescriptType(resources[i].Fields)
			t.resources = append(t.resources, resources[i])
		}
	}

	if t.genRPCMethods {
		rpcStructs := parser.ParsePackage(packageMap["rpc"]).Structs

		rpcStructs = parser.FilterStructsByInterface(rpcStructs, rpcInterfaces[:])

		t.rpcMethods, err = t.structsToRPCMethods(rpcStructs)
		if err != nil {
			return err
		}

		for i := range t.rpcMethods {
			t.rpcMethods[i].Fields = t.rpcFieldsTypescriptType(t.rpcMethods[i].Fields)
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
		if err := RemoveGeneratedFiles(t.typescriptDestination, HeaderComment); err != nil {
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
		if err := RemoveGeneratedFiles(t.typescriptDestination, HeaderComment); err != nil {
			return errors.Wrap(err, "RemoveGeneratedFiles()")
		}
	}

	log.Println("Starting typescript resource permission generation...")

	routerData := t.rc.TypescriptData()

	templateData := map[string]any{
		"Permissions":            routerData.Permissions,
		"ResourcePermissions":    routerData.ResourcePermissions,
		"Resources":              routerData.Resources,
		"ResourceTags":           routerData.ResourceTags,
		"ResourcePermissionsMap": routerData.ResourcePermissionMap,
		"Domains":                routerData.Domains,
	}

	if t.genRPCMethods {
		templateData["RPCMethods"] = t.rpcMethods
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
	if err := RemoveGeneratedFiles(t.typescriptDestination, HeaderComment); err != nil {
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
		"RPCMethods": t.rpcMethods,
		"GenPrefix": genPrefix,
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

func (t *typescriptGenerator) resourceFieldsTypescriptType(fields []resourceField) []resourceField {
	for i := range fields {
		if override, ok := t.typescriptOverrides[fields[i].TypeName()]; ok {
			fields[i].typescriptType = override
		} else {
			fields[i].typescriptType = "string"
		}

		if fields[i].IsIterable() {
			fields[i].typescriptType += "[]"
		}

		if fields[i].IsForeignKey && slices.Contains(t.routerResources, accesstypes.Resource(fields[i].ReferencedResource)) {
			fields[i].IsEnumerated = true
		}
	}

	return fields
}

func (t *typescriptGenerator) rpcFieldsTypescriptType(fields []rpcField) []rpcField {
	for i := range fields {
		if override, ok := t.typescriptOverrides[fields[i].TypeName()]; ok {
			fields[i].typescriptType = override
		} else {
			fields[i].typescriptType = "string"
		}

		if fields[i].IsIterable() {
			fields[i].typescriptType += "[]"
		}
	}

	return fields
}
