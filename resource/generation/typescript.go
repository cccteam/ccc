package generation

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"slices"
	"text/template"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
)

type typescriptGenerator struct {
	*client
	genPermission          bool
	genMetadata            bool
	typescriptDestination  string
	typescriptOverrides    map[string]string
	rc                     *resource.Collection
	routerResources        []accesstypes.Resource
	spannerEmulatorVersion string
}

func NewTypescriptGenerator(ctx context.Context, resourceSourcePath, migrationSourceURL string, targetDir string, rc *resource.Collection, mode TSGenMode, options ...TSOption) (Generator, error) {
	if rc == nil {
		return nil, errors.New("resource collection cannot be nil")
	}

	var (
		genPermission bool
		genMetadata   bool
	)
	switch mode {
	case TSPerm | TSMeta:
		genPermission = true
		genMetadata = true
	case TSPerm:
		genPermission = true
	case TSMeta:
		genMetadata = true
	}

	t := &typescriptGenerator{
		rc:                    rc,
		routerResources:       rc.Resources(),
		typescriptDestination: targetDir,
		genPermission:         genPermission,
		genMetadata:           genMetadata,
	}

	opts := make([]option, 0, len(options))
	for _, opt := range options {
		opts = append(opts, opt)
	}

	c, err := newClient(ctx, resourceSourcePath, migrationSourceURL, nil, opts)
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

	packageMap, err := parser.LoadPackages(t.loadPackages...)
	if err != nil {
		return err
	}

	resources, err := t.extractResources(packageMap["resources"])
	if err != nil {
		return err
	}

	t.resources = make([]*resourceInfo, 0, len(resources))
	for _, resourceInfo := range resources {
		resource := accesstypes.Resource(t.pluralize(resourceInfo.Name()))
		if t.rc.ResourceExists(resource) {
			t.resources = append(t.resources, t.setResourceTypescriptInfo(resourceInfo))
		}
	}

	if t.genRPCMethods {
		rpcStructs := parser.ParseStructs(packageMap["rpc"])

		rpcStructs = parser.FilterStructsByInterface(rpcStructs, rpcInterfaces[:])

		t.rpcMethods = nil
		for _, s := range rpcStructs {
			methodInfo, err := t.structToRPCMethod(s)
			if err != nil {
				return err
			}
			methodInfo = t.setMethodTypescriptInfo(methodInfo)
			t.rpcMethods = append(t.rpcMethods, methodInfo)
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

	return nil
}

func (t *typescriptGenerator) runTypescriptPermissionGeneration() error {
	if !t.genMetadata {
		if err := removeGeneratedFiles(t.typescriptDestination, HeaderComment); err != nil {
			return errors.Wrap(err, "removeGeneratedFiles()")
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

	output, err := t.generateTemplateOutput(typescriptConstantsTemplate, templateData)
	if err != nil {
		return errors.Wrap(err, "c.generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, "constants.ts")
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated Permissions: %s\n", file.Name())

	return nil
}

func (t *typescriptGenerator) runTypescriptMetadataGeneration() error {
	if err := removeGeneratedFiles(t.typescriptDestination, HeaderComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	if err := t.generateTypescriptMetadata(); err != nil {
		return errors.Wrap(err, "generateTypescriptResources")
	}

	return nil
}

func (t *typescriptGenerator) generateTemplateOutput(fileTemplate string, data map[string]any) ([]byte, error) {
	tmpl, err := template.New(fileTemplate).Funcs(t.templateFuncs()).Parse(fileTemplate)
	if err != nil {
		return nil, errors.Wrap(err, "template.Parse()")
	}

	buf := bytes.NewBuffer([]byte{})
	if err := tmpl.Execute(buf, data); err != nil {
		return nil, errors.Wrap(err, "tmpl.Execute()")
	}

	return buf.Bytes(), nil
}

func (t *typescriptGenerator) generateTypescriptMetadata() error {
	log.Println("Starting typescript metadata generation...")

	if err := t.generateResourceMetadata(); err != nil {
		return errors.Wrap(err, "generateResourceMetadata()")
	}

	if err := t.generateMethodMetadata(); err != nil {
		return errors.Wrap(err, "generateMethodMetadata()")
	}

	log.Println("Generated typescript metadata")

	return nil
}

func (t *typescriptGenerator) generateResourceMetadata() error {
	log.Println("Starting resource metadata generation...")
	output, err := t.generateTemplateOutput(typescriptResourcesTemplate, map[string]any{
		"Resources":         t.resources,
		"ConsolidatedRoute": t.consolidatedRoute,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, "resources.ts")
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated resource metadata: %s\n", file.Name())

	return nil
}

func (t *typescriptGenerator) generateMethodMetadata() error {
	log.Println("Starting method metadata generation...")

	output, err := t.generateTemplateOutput(typescriptMethodsTemplate, map[string]any{
		"RPCMethods": t.rpcMethods,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, "methods.ts")
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated methods metadata: %s\n", file.Name())

	return nil
}

func (t *typescriptGenerator) setResourceTypescriptInfo(resource *resourceInfo) *resourceInfo {
	for _, field := range resource.Fields {
		field.typescriptType = t.typescriptType(field)

		if field.IsForeignKey && slices.Contains(t.routerResources, accesstypes.Resource(field.ReferencedResource)) {
			field.IsEnumerated = true
		}
	}

	return resource
}

func (t *typescriptGenerator) setMethodTypescriptInfo(method *rpcMethodInfo) *rpcMethodInfo {
	for _, field := range method.Fields {
		field.typescriptType = t.typescriptType(field)
	}

	return method
}

func (t *typescriptGenerator) typescriptType(field field) string {
	var tsType string
	if override, ok := t.typescriptOverrides[field.TypeName()]; ok {
		tsType = override
	} else {
		tsType = "string"
	}

	if field.IsIterable() {
		tsType += "[]"
	}

	return tsType
}
