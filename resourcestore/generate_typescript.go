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
	Domains             []accesstypes.PermissionScope
}

const tmpl = `// This file is auto-generated. Do not edit manually.
import { Domain, Permission, Resource } from '@cccteam/ccc-types';

{{- $permissions := .Permissions }}
{{- $resources := .Resources }}
{{- $resourcetags := .ResourceTags }}
{{- $resourcePerms := .ResourcePermissions }}
{{- $domains := .Domains }}
export const Permissions = {
{{- range $permissions }}
	{{.}}: '{{.}}' as Permission,
{{- end}}
};

export const Domains = {
{{- range $domains }}
	{{.}}: '{{.}}' as Domain,
{{- end}}
};

export const Resources = {
{{- range $resource := $resources }}
	{{ $resource }}: '{{ $resource }}' as Resource,
{{- end}}
};
{{ range $resource, $tags := $resourcetags }}
export const {{ $resource }} = {
{{- range $_, $tag := $tags }}
	{{ $tag }}: '{{ $resource.ResourceWithTag $tag }}' as Resource,
{{- end }}
};
{{ end }}
type PermissionResources = Record<Permission, boolean>;
type PermissionMappings = Record<Resource, PermissionResources>;

const Mappings: PermissionMappings = {
	{{- range $resource := $resources }}
	[Resources.{{ $resource }}]: {
		{{- range $perm := $permissions }}
		[Permissions.{{ $perm }}]: {{ index $resourcePerms $resource $perm }},
		{{- end }}
	},
		{{- range $tag := index $resourcetags $resource }}
	[{{$resource.ResourceWithTag $tag }}]: {
			{{- range $perm := $permissions }}
		[Permissions.{{ $perm }}]: {{ index $resourcePerms ($resource.ResourceWithTag $tag) $perm }},
			{{- end }}
	},
		{{- end }}
	{{- end }}
};

export function requiresPermission(resource: Resource, permission: Permission): boolean {
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
		Domains:             s.domains(),
	}); err != nil {
		return errors.Wrap(err, "template.Template.Execute()")
	}

	if err := f.Close(); err != nil {
		return errors.Wrap(err, "os.file.Close()")
	}

	return nil
}
