package resourcestore

import (
	"os"
	"text/template"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type TSGenerator struct {
	Permissions         []accesstypes.Permission
	Resources           []accesstypes.Resource
	ResourceTags        map[accesstypes.Resource][]accesstypes.Tag
	ResourcePermissions permissionMap
}

const tmpl = `// This file is auto-generated. Do not edit manually.
{{- $permissions := .Permissions }}
{{- $resources := .Resources}}
{{- $resourcetags := .ResourceTags }}
{{- $resourcePerms := .ResourcePermissions}}
export enum Permissions {
{{- range $permissions}}
	{{.}} = '{{.}}',
{{- end}}
}

export enum Resources {
{{- range $resource := $resources}}
	{{$resource}} = '{{$resource}}',
{{- end}}
}
{{ range $resource, $tags := $resourcetags}}
export enum {{$resource}} {
	{{- range $_, $tag:= $tags}}
	{{$tag}} = '{{$resource.ResourceWithTag $tag}}',
	{{- end}}
}
{{ end}}
type AllResources = Resources {{- range $resource := .Resources}} | {{$resource}}{{- end}};
type PermissionResources = Record<Permissions, boolean>;
type PermissionMappings = Record<AllResources, PermissionResources>;

const Mappings: PermissionMappings = {
	{{- range $resource := $resources}}
	[Resources.{{$resource}}]: {
		{{- range $perm := $permissions}}
		[Permissions.{{$perm}}]: {{- index $resourcePerms $resource $perm}},
		{{- end}}
	},
		{{- range $tag := index $resourcetags $resource}}
	[{{$resource.ResourceWithTag $tag}}]: {
			{{- range $perm := $permissions}}
		[Permissions.{{$perm}}]: {{- index $resourcePerms ($resource.ResourceWithTag $tag) $perm}},
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

	tsFile := template.Must(template.New("").Parse(tmpl))

	if err := tsFile.Execute(f, TSGenerator{
		Permissions:         s.permissions(),
		Resources:           s.resources(),
		ResourceTags:        s.tags(),
		ResourcePermissions: s.resourcePermissions(),
	}); err != nil {
		return errors.Wrap(err, "template.Template.Execute()")
	}

	if err := f.Close(); err != nil {
		return errors.Wrap(err, "os.file.Close()")
	}

	return nil
}
