package resourcestore

import (
	"html/template"
	"os"

	"github.com/cccteam/ccc/accesstypes"
)

type TSGenerator struct {
	Permissions []accesstypes.Permission
	Resources   []accesstypes.Resource
	Mappings    map[accesstypes.Permission]map[accesstypes.Resource]bool
}

const tmpl = `// This file is auto-generated. Do not edit manually.
export enum Permissions {
{{- range .Permissions}}
  {{.}} = '{{.}}',
{{- end}}
}

export enum Resources {
{{- range .Resources}}
  {{.}} = '{{.}}',
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
	tsFile, err := template.New("").Parse(tmpl)
	if err != nil {
		panic(err)
	}

	if err := tsFile.Execute(os.Stdout, TSGenerator{
		Permissions: s.permissions(),
		Resources:   s.resources(),
		Mappings:    map[accesstypes.Permission]map[accesstypes.Resource]bool{},
	}); err != nil {
		panic(err)
	}

	return nil
}
