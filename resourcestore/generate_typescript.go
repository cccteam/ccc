package resourcestore

import (
	"html/template"
	"os"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type TSGenerator struct {
	Permissions []accesstypes.Permission
	Resources   map[accesstypes.Resource]struct{}
	Tags        map[accesstypes.Resource][]accesstypes.Tag
	Mappings    map[accesstypes.Resource]map[accesstypes.Permission]bool
}

const tmpl = `// This file is auto-generated. Do not edit manually.

{{- $permissions := .Permissions }}
{{- $resources := .Resources}}
{{- $resourcetags := .Tags }}
{{- $permissionmap := .Mappings}}

export enum Permissions {
{{- range $permissions}}
  {{.}} = '{{.}}',
{{- end}}
}

export enum Resources {
{{- range $resource, $_ := $resources}}
  {{$resource}} = '{{$resource}}',
{{- end}}
}

{{- range $resource, $tags := $resourcetags}}
export enum {{$resource}} {
	{{- range $_, $tag:= $tags}}
		{{$tag}} = '{{$resource.ResourceWithTag $tag}}',
	{{- end}}
}

{{- end}}

type AllResources = Resources {{- range $resource, $_ := .Resources}} | {{$resource}}{{- end}};
type PermissionResources = Record<Permissions, boolean>;
type PermissionMappings = Record<AllResources, PermissionResources>;

const Mappings: PermissionMappings = {
	{{- range $resource, $_ := $resources}}
	[Resources.{{$resource}}]: {
		{{- range $perm := $permissions}}
		[Permissions.{{$perm}}]: {{index $permissionmap $resource $perm}},
		{{- end}}
	},
		{{- range $tag := index $resourcetags $resource}}		
	[{{$resource.ResourceWithTag $tag}}]: {
			{{- range $perm := $permissions}}
		[Permissions.{{$perm}}]: {{index $permissionmap $resource $perm}},
			{{- end}}
	},
		{{- end}}
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
		Tags:        s.tags(),
		Mappings:    s.requiredPermissionsMap(),
	}); err != nil {
		panic(err)
	}

	if err := f.Close(); err != nil {
		return err
	}

	return err
}
