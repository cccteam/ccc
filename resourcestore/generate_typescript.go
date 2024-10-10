package resourcestore

import (
	"html/template"
	"os"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type TSGenerator struct {
	Permissions []accesstypes.Permission
	Resources   map[string]accesstypes.Resource
	Mappings    map[accesstypes.Permission]map[string]bool
}

const tmpl = `// This file is auto-generated. Do not edit manually.
export enum Permissions {
{{- range .Permissions}}
  {{.}} = '{{.}}',
{{- end}}
}

export enum Resources {
{{- range $enum, $resource := .Resources}}
  {{$enum}} = '{{$resource}}',
{{- end}}
}

type ResourcePermissions = Record<Resources, boolean>;
type PermissionMappings = Record<Permissions, ResourcePermissions>;

const Mappings: PermissionMappings = {
{{- range $perm, $resources := .Mappings}}
  [Permissions.{{$perm}}]: {
  {{- range $resource, $required := $resources}}
    [Resources.{{$resource}}]: {{$required}},
  {{- end}}
  },
{{- end}}
};

export function hasPermission(permission: Permissions, resource: Resources): boolean {
  return Mappings[permission][resource];
}
`

func (s *Store) GenerateTypeScript(dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer f.Close()

	tsFile, err := template.New("").Parse(tmpl)
	if err != nil {
		panic(err)
	}

	if err := tsFile.Execute(f, TSGenerator{
		Permissions: s.permissions(),
		Resources:   s.resources(),
		Mappings:    s.permissionResources(),
	}); err != nil {
		panic(err)
	}

	if err := f.Close(); err != nil {
		return err
	}

	return err
}
