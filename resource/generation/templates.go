package generation

import "fmt"

var (
	resourcesInterfaceTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

package resources

import (
	"github.com/cccteam/ccc/resource"
)

type Resource interface {
	resource.Resourcer
{{ FormatResourceInterfaceTypes .Types }}
}`

	resourceFileTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

package resources

import (
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/queryset"
	"github.com/cccteam/patcher"
	"github.com/go-playground/errors/v5"
)

const {{ Pluralize .Name }} accesstypes.Resource = "{{ Pluralize .Name }}"

func ({{ .Name }}) Resource() accesstypes.Resource {
	return {{ Pluralize .Name }}
}

func ({{ .Name }}) DefaultConfig() resource.Config {
	return defaultConfig()
}

type {{ .Name }}Query struct {
	qSet *resource.QuerySet[{{ .Name }}]
}

func New{{ .Name }}Query() *{{ .Name }}Query {
	return &{{ .Name }}Query{qSet: resource.NewQuerySet(resource.NewResourceMetadata[{{ .Name }}]())}
}

func New{{ .Name }}QueryFromQuerySet(qSet *resource.QuerySet[{{ .Name }}]) *{{ .Name }}Query {
	return &{{ .Name }}Query{qSet: qSet}
}

{{ $TypeName := .Name}}
{{ range .Fields }}
{{ if eq .IsIndex true }}
func (q *{{ $TypeName }}Query) Set{{ .Name }}(v {{ .Type }}) *{{ $TypeName }}Query {
	q.qSet.SetKey("{{ .Name }}", v)

	return q
}

func (q *{{ $TypeName }}Query) {{ .Name }}() {{ .Type }} {
	v, _ := q.qSet.Key("{{ .Name }}").({{ .Type }})

	return v
}
{{ end }}
{{ end }}

{{ if ne (len .SearchIndexes) 0 }}
{{ range .SearchIndexes }}
func (q *{{ $TypeName }}Query) SearchBy{{ .Name }}(v string) *{{ $TypeName }}Query {
	searchSet := resource.NewSearchSet({{ ResourceSearchType .SearchType }}, "{{ .Name }}", v)
	q.qSet.SetSearchParam(searchSet)

	return q
}
{{ end }}
{{ end }}

func (q *{{ .Name }}Query) Query() *resource.QuerySet[{{ .Name }}] {
	return q.qSet
}

func (q *{{ .Name }}Query) AddAllColumns() *{{ .Name }}Query {
	{{- range .Fields }}
	q.qSet.AddField("{{ .Name }}")
	{{- end }}

	return q
}

{{ $TypeName := .Name}}
{{ range .Fields }}
func (q *{{ $TypeName }}Query) AddColumn{{ .Name }}() *{{ $TypeName }}Query {
	q.qSet.AddField("{{ .Name }}")

	return q
}
{{ end }}

{{ if eq .IsView false }}
type {{ .Name }}CreatePatch struct {
	patchSet *resource.PatchSet[{{ .Name }}]
}

{{ $PrimaryKeyIsUUID := PrimaryKeyTypeIsUUID .Fields }}
{{ if and (eq .HasCompoundPrimaryKey false) (eq $PrimaryKeyIsUUID true) }}
func New{{ .Name }}CreatePatchFromPatchSet(patchSet *resource.PatchSet[{{ .Name }}]) (*{{ .Name }}CreatePatch, error) {
	id, err := ccc.NewUUID()
	if err != nil {
		return nil, errors.Wrap(err, "ccc.NewUUID()")
	}
	
	patchSet.
		SetKey("ID", id).
		SetPatchType(resource.CreatePatchType)
	
	return &{{ .Name }}CreatePatch{patchSet: patchSet}, nil
}

func New{{ .Name }}CreatePatch() (*{{ .Name }}CreatePatch, error) {
	id, err := ccc.NewUUID()
	if err != nil {
		return nil, errors.Wrap(err, "ccc.NewUUID()")
	}
	
	patchSet := resource.NewPatchSet(resource.NewResourceMetadata[{{ .Name }}]()).
		SetKey("ID", id).
		SetPatchType(resource.CreatePatchType)

	return &{{ .Name }}CreatePatch{patchSet: patchSet}, nil
}
{{ else }}
func New{{ .Name }}CreatePatchFromPatchSet(
{{- range $i, $e := .Fields }}{{ if eq .IsPrimaryKey true }}{{ GoCamel .Name }} {{ .Type }},{{ end }}{{ end }} patchSet *resource.PatchSet[{{ .Name }}]) *{{ .Name }}CreatePatch {
	patchSet.
	{{ range .Fields }}
	{{ if eq .IsPrimaryKey true }}
	 	SetKey("{{ .Name }}", {{ GoCamel .Name }}).
	{{ end }}
	{{ end }}
		SetPatchType(resource.CreatePatchType)
	
	return &{{ .Name }}CreatePatch{patchSet: patchSet}
}

func New{{ .Name }}CreatePatch(
{{- range $i, $e := .Fields }}
{{- if eq .IsPrimaryKey true }}{{- if $i }}, {{ end }}{{ GoCamel .Name }} {{ .Type }}{{ end }}{{ end }}) *{{ .Name }}CreatePatch {
	patchSet := resource.NewPatchSet(resource.NewResourceMetadata[{{ .Name }}]()).
	{{ range .Fields }}
	{{ if eq .IsPrimaryKey true }}
	 	SetKey("{{ .Name }}", {{ GoCamel .Name }}).
	{{ end }}
	{{ end }}
		SetPatchType(resource.CreatePatchType)

	return &{{ .Name }}CreatePatch{patchSet: patchSet}
}
{{ end }}

func (p *{{ .Name }}CreatePatch) PatchSet() *resource.PatchSet[{{ .Name }}] {
	return p.patchSet
}

` + fieldAccessors(CreatePatch) + `

type {{ .Name }}UpdatePatch struct {
	patchSet *resource.PatchSet[{{ .Name }}]
}

func New{{ .Name }}UpdatePatchFromPatchSet(
{{- range $i, $e := .Fields }}
{{- if eq .IsPrimaryKey true }}{{ GoCamel .Name }} {{ .Type }},{{ end }}
{{- end }}patchSet *resource.PatchSet[{{ .Name }}]) *{{ .Name }}UpdatePatch {
	patchSet.
	{{- range .Fields }}
	{{- if eq .IsPrimaryKey true }}
		SetKey("{{ .Name }}", {{ GoCamel .Name }}).
	{{- end }}
	{{- end }}
		SetPatchType(resource.UpdatePatchType)
	
	return &{{ .Name }}UpdatePatch{patchSet: patchSet}
}

func New{{ .Name }}UpdatePatch(
{{- range $i, $e := .Fields }}
{{- if eq .IsPrimaryKey true }}{{- if $i }}, {{ end }}{{ GoCamel .Name }} {{ .Type }}{{ end }}{{ end }}) *{{ .Name }}UpdatePatch {
	patchSet := resource.NewPatchSet(resource.NewResourceMetadata[{{ .Name }}]()).
	{{- range .Fields }}
	{{- if eq .IsPrimaryKey true }}
		SetKey("{{ .Name }}", {{ GoCamel .Name }}).
	{{- end }}
	{{- end }}
		SetPatchType(resource.UpdatePatchType)
	
	return &{{ .Name }}UpdatePatch{patchSet: patchSet}
}

func (p *{{ .Name }}UpdatePatch) PatchSet() *resource.PatchSet[{{ .Name }}] {
	return p.patchSet
}

` + fieldAccessors(UpdatePatch) + `

type {{ .Name }}DeletePatch struct {
	patchSet *resource.PatchSet[{{ .Name }}]
}

func New{{ .Name }}DeletePatch(
{{- range $i, $e := .Fields }}
{{- if eq .IsPrimaryKey true }}{{- if $i }}, {{ end }}{{ GoCamel .Name }} {{ .Type}}{{ end }}{{ end }}) *{{ .Name }}DeletePatch {
	patchSet := resource.NewPatchSet(resource.NewResourceMetadata[{{ .Name }}]()).
	{{- range .Fields }}
	{{- if eq .IsPrimaryKey true }}
		SetKey("{{ .Name }}", {{ GoCamel .Name }}).
	{{- end }}
	{{- end }}
		SetPatchType(resource.DeletePatchType)
	
	return &{{ .Name }}DeletePatch{patchSet: patchSet}
}

func (p *{{ .Name }}DeletePatch) PatchSet() *resource.PatchSet[{{ .Name }}] {
	return p.patchSet
}

{{ $TypeName := .Name}}
{{ range .Fields }}
{{ if eq .IsPrimaryKey true }} 
func (p *{{ $TypeName }}DeletePatch) {{ .Name }}() {{ .Type }} {
	v, _ := p.patchSet.Key("{{ .Name }}").({{ .Type }}) 

	return v
}
{{ end }}
{{ end }}
{{ end }}`
)

const (
	handlerHeaderTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

package app

{{ .Handlers }}`

	listTemplate = `func (a *App) {{ Pluralize .Type.Name }}() http.HandlerFunc {
	{{ $StructName := Pluralize .Type.Name -}}
	type {{ GoCamel .Type.Name }} struct {
		{{ range .Type.Fields }}{{ .Name }} {{ .Type }} ` + "`json:\"{{ DetermineJSONTag . false }}\"{{ if eq .IsIndex true }}index:\"true\"{{end}}{{ FormatPerm .ListPerm }}{{ FormatQueryTag .QueryTag }}{{ FormatTokenTag $StructName .SpannerColumn }}`" + `
		{{ end }}
	}

	type response []*{{ GoCamel .Type.Name }}

	decoder := NewQueryDecoder[resources.{{ .Type.Name }}, {{ GoCamel .Type.Name }}](a, accesstypes.List)

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := otel.Tracer(name).Start(r.Context(), "App.{{ Pluralize .Type.Name }}()")
		defer span.End()

		querySet, err := decoder.Decode(r)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		rows, err := spanner.List(ctx, a.businessLayer.DB(), resources.New{{ .Type.Name }}QueryFromQuerySet(querySet))
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		resp := make(response, 0, len(rows))
		for _, r := range rows {
			resp = append(resp, (*{{ GoCamel .Type.Name }})(r))
		}

		return httpio.NewEncoder(w).Ok(resp)
	})
}`

	readTemplate = `func (a *App) {{ .Type.Name }}() http.HandlerFunc {
	{{ $StructName := Pluralize .Type.Name -}}
	type response struct {
		{{ range .Type.Fields }}{{ .Name }} {{ .Type }} ` + "`json:\"{{ DetermineJSONTag . false }}\"{{ if eq .IsUniqueIndex true }}index:\"true\"{{end}}{{ FormatPerm .ReadPerm }}{{ FormatQueryTag .QueryTag }}{{ FormatTokenTag $StructName .SpannerColumn }}`" + `
		{{ end }}
	}

	decoder := NewQueryDecoder[resources.{{ .Type.Name }}, response](a, accesstypes.Read)

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := otel.Tracer(name).Start(r.Context(), "App.{{ .Type.Name }}()")
		defer span.End()

		id := httpio.Param[{{ PrimaryKeyType .Type.Fields }}](r, router.{{ .Type.Name }}ID)

		querySet, err := decoder.Decode(r)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		row, err := spanner.Read(ctx, a.businessLayer.DB(), resources.New{{ .Type.Name }}QueryFromQuerySet(querySet).SetID(id))
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		return httpio.NewEncoder(w).Ok((*response)(row))
	})
}`

	patchTemplate = `func (a *App) Patch{{ Pluralize .Type.Name }}() http.HandlerFunc {
	type request struct {
		{{ range .Type.Fields }}{{ .Name }} {{ .Type }} ` + "`json:\"{{ DetermineJSONTag . true }}\"{{ FormatPerm .PatchPerm }}{{ FormatQueryTag .QueryTag }}`" + `
		{{ end }}
	}
	
	{{ $PrimaryKeyType := PrimaryKeyType .Type.Fields }}
	{{- if eq $PrimaryKeyType "ccc.UUID"  }}
	type response struct {
		IDs []ccc.UUID ` + "`json:\"iDs\"`" + `
	}
	{{- end }}

	decoder := NewDecoder[resources.{{ .Type.Name }}, request](a, accesstypes.Create, accesstypes.Update, accesstypes.Delete)

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := otel.Tracer(name).Start(r.Context(), "App.Patch{{ Pluralize .Type.Name }}()")
		defer span.End()

		var patches []resource.SpannerBufferer
		{{- if eq $PrimaryKeyType "ccc.UUID"  }}
		var resp response
		{{- end }}

		for op, err := range resource.Operations(r, "/{id}"{{- if ne $PrimaryKeyType "ccc.UUID"  }}, resource.RequireCreatePath(){{- end }}) {
			if err != nil {
				return httpio.NewEncoder(w).ClientMessage(ctx, err)
			}

			patchSet, err := decoder.DecodeOperation(op)
			if err != nil {
				return httpio.NewEncoder(w).ClientMessage(ctx, err)
			}
			
			switch op.Type {
			case resource.OperationCreate:
				{{- if eq $PrimaryKeyType "ccc.UUID" }}
				patch, err := resources.New{{ .Type.Name }}CreatePatchFromPatchSet(patchSet)
				if err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
				patches = append(patches, patch.PatchSet())
				resp.IDs = append(resp.IDs, patch.ID())
				{{- else }}
				id := httpio.Param[{{ $PrimaryKeyType }}](op.Req, "id")
				patches = append(patches, resources.New{{ .Type.Name }}CreatePatchFromPatchSet(id, patchSet).PatchSet())
				{{- end }}
			case resource.OperationUpdate:
				id := httpio.Param[{{ $PrimaryKeyType }}](op.Req, "id")
				patches = append(patches, resources.New{{ .Type.Name }}UpdatePatchFromPatchSet(id, patchSet).PatchSet())
			case resource.OperationDelete:
				id := httpio.Param[{{ $PrimaryKeyType }}](op.Req, "id")
				patches = append(patches, resources.New{{ .Type.Name }}DeletePatch(id).PatchSet())
			}
		}

		if err := a.businessLayer.DB().Patch(ctx, resource.UserEvent(ctx), patches...); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, spanner.HandleError[resources.{{ .Type.Name }}](err))
		}

		{{ if eq $PrimaryKeyType "ccc.UUID"  }}
		return httpio.NewEncoder(w).Ok(resp)
		{{ else }}
		return httpio.NewEncoder(w).Ok(nil)
		{{- end -}}
	})
}`

	resourcesTestTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
	// Source: {{ .Source }}

	package resources_test

	import (
		"testing"
	)

	func TestClient_Resources(t *testing.T) {
		t.Parallel()

		{{ range .Types }}
		RunResourceTestsFor[resources.{{ .Name }}](t)
		{{- end }}
	}`

	typescriptPermissionTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
import { Domain, Permission, Resource } from '@cccteam/ccc-lib';
{{- $permissions := .Permissions }}
{{- $resources := .Resources }}
{{- $resourcetags := .ResourceTags }}
{{- $resourcePerms := .ResourcePermissions }}
{{- $domains := .Domains }}

export const Permissions = {
{{- range $perm := $permissions }}
  {{ $perm }}: '{{ $perm }}' as Permission,
{{- end}}
};

export const Domains = {
{{- range $domain := $domains }}
  {{ $domain }}: '{{ $domain }}' as Domain,
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

	typescriptMetadataTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
import { Resource } from '@cccteam/ccc-lib';
import { Link, ResourceMap, ResourceMeta } from '@components/Resource/resources-helpers';
import { Resources } from './resourcePermissions';
{{- $resources := .Resources }}
{{ range $resource := $resources }}
export interface {{ Pluralize $resource.Name }} {
{{- range $field := $resource.Fields }}
  {{ Camel $field.Name }}: {{ $field.DataType }};
{{- end }}
}
{{ end }}
const resourceMap: ResourceMap = {
  {{- range $resource := $resources }}
  [Resources.{{ Pluralize $resource.Name }}]: {
    route: '{{ Kebab (Pluralize $resource.Name) }}',
    fields: [
      {{- range $field := $resource.Fields }}
      { fieldName: '{{ Camel $field.Name }}', {{- if $field.IsPrimaryKey }} primaryKey: { ordinalPosition: {{ $field.KeyOrdinalPosition }} },{{- end }} displayType: '{{ Lower $field.DisplayType }}', required: {{ $field.Required }}{{ if $field.IsForeignKey }}, enumeratedResource: Resources.{{ $field.ReferencedResource }}{{ end }} },
      {{- end }}
    ],
  },

  {{- end }}
}

export function resourceMeta(resource: Resource): ResourceMeta {
  if (resourceMap[resource] !== undefined) {
    return resourceMap[resource];
  } else {
    console.error('Resource not found in resourceMap:', resource);
    return {} as ResourceMeta;
  }
}
`
)

func fieldAccessors(patchType PatchType) string {
	return fmt.Sprintf(`{{ $TypeName := .Name}}
		{{ range .Fields }}
		{{ if eq .IsPrimaryKey false }}
		func (p *{{ $TypeName }}%[1]sPatch) Set{{ .Name }}(v {{ .Type }}) *{{ $TypeName }}%[1]sPatch {
			p.patchSet.Set("{{ .Name }}", v)

			return p
		}
		{{ end }}

		func (p *{{ $TypeName }}%[1]sPatch) {{ .Name }}() {{ .Type }} {
		{{ if eq .IsPrimaryKey true -}} 
			v, _ := p.patchSet.Key("{{ .Name }}").({{ .Type }})
		{{ else -}} 
			v, _ := p.patchSet.Get("{{ .Name }}").({{ .Type }}) 
		{{ end }}

			return v
		}

		{{ if eq .IsPrimaryKey false }}
		func (p *{{ $TypeName }}%[1]sPatch) {{ .Name }}IsSet() bool {
			return p.patchSet.IsSet("{{ .Name }}")
		}
		{{ end }}
		{{ end }}`, string(patchType))
}
