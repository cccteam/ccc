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
	Tags        map[string]accesstypes.Resource
	Mappings    map[string]map[accesstypes.Permission]bool
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

{{- range _, $resource := .Resources}}
export enum $resource {
	{{- range $tag, $permission := .Tags}}
		{{$tag}} = '{{$permission}}',
	{{- end}}
}
{{- end}}

type AllResources = Resources {{- range _, $resource := .Resources}}| {{$resource}}{{- end}};
type PermissionResources = Record<Permissions, boolean>;
type PermissionMappings = Record<AllResources, PermissionResources>;

const Mappings: PermissionMappings = {
{{- range $perm, $resources := .Mappings}}
  [Resources.{{$perm}}]: {
  {{- range $resource, $required := $resources}}
    [Permissions.{{$resource}}]: {{$required}},
  {{- end}}
  },
{{- end}}
};

export function requiresPermission(resource: AllResources, permission: Permissions): boolean {
  return Mappings[resource][permission];
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
