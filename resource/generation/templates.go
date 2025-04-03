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
	"reflect"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/queryset"
	"github.com/cccteam/patcher"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

const {{ Pluralize .Resource.Name }} accesstypes.Resource = "{{ Pluralize .Resource.Name }}"

func ({{ .Resource.Name }}) Resource() accesstypes.Resource {
	return {{ Pluralize .Resource.Name }}
}

func ({{ .Resource.Name }}) DefaultConfig() resource.Config {
	return defaultConfig()
}

type {{ .Resource.Name }}Query struct {
	qSet *resource.QuerySet[{{ .Resource.Name }}]
}

func New{{ .Resource.Name }}Query() *{{ .Resource.Name }}Query {
	return &{{ .Resource.Name }}Query{qSet: resource.NewQuerySet(resource.NewResourceMetadata[{{ .Resource.Name }}]())}
}

func New{{ .Resource.Name }}QueryFromQuerySet(qSet *resource.QuerySet[{{ .Resource.Name }}]) *{{ .Resource.Name }}Query {
	return &{{ .Resource.Name }}Query{qSet: qSet}
}

{{ range $field := .Resource.Fields }}
{{ if $field.IsUniqueIndex }}
func (q *{{ $field.Parent.Name }}Query) Set{{ $field.Name }}(v {{ .Type }}) *{{ $field.Parent.Name }}Query {
	q.qSet.SetKey("{{ $field.Name }}", v)

	return q
}

func (q *{{ $field.Parent.Name }}Query) {{ $field.Name }}() {{ $field.Type }} {
	v, _ := q.qSet.Key("{{ $field.Name }}").({{ $field.Type }})

	return v
}
{{ end }}
{{ end }}

{{ if ne (len .Resource.SearchIndexes) 0 }}
{{ $resource := .Resource }}
{{ range $searchIndex := .Resource.SearchIndexes }}
func (q *{{ $resource.Name }}Query) SearchBy{{ $searchIndex.Name }}(v string) *{{ $resource.Name }}Query {
	searchSet := resource.NewFilter({{ ResourceSearchType $searchIndex.SearchType }}, map[resource.FilterKey]string{"{{ $searchIndex.Name }}": v}, nil)
	q.qSet.SetFilterParam(searchSet)

	return q
}
{{ end }}
{{ end }}

func (q *{{ .Resource.Name }}Query) Query() *resource.QuerySet[{{ .Resource.Name }}] {
	return q.qSet
}

func (q *{{ .Resource.Name }}Query) AddAllColumns() *{{ .Resource.Name }}Query {
	{{- range $field := .Resource.Fields }}
	q.qSet.AddField("{{ $field.Name }}")
	{{- end }}

	return q
}


{{ range $field := .Resource.Fields }}
func (q *{{ $field.Parent.Name }}Query) AddColumn{{ $field.Name }}() *{{ $field.Parent.Name }}Query {
	q.qSet.AddField("{{ $field.Name }}")

	return q
}
{{ end }}

{{ if .Resource.HasIndexes -}}
func (q *{{ .Resource.Name }}Query) Where(c {{ .Resource.Name }}QueryClause) *{{ .Resource.Name }}Query {
	q.qSet.SetWhereClause(c.clause)

	return q
}

type {{ .Resource.Name }}QueryPartialClause struct {
	partialClause resource.PartialQueryClause
}

func New{{ .Resource.Name }}QueryClause() {{ .Resource.Name }}QueryPartialClause {
	return {{ .Resource.Name }}QueryPartialClause{partialClause: resource.NewPartialQueryClause()}
}

func (p {{ .Resource.Name }}QueryPartialClause) Group(qc {{ .Resource.Name }}QueryClause) {{ .Resource.Name }}QueryClause {
	return {{ .Resource.Name }}QueryClause{clause: p.partialClause.Group(qc.clause)}
}

{{ range $field := .Resource.Fields }}
{{ if or $field.IsIndex $field.IsUniqueIndex -}}
func (p {{ $field.Parent.Name }}QueryPartialClause) {{ $field.Name }}() {{ $field.Parent.Name }}QueryIdent[{{ $field.Type }}] {
	return {{ $field.Parent.Name }}QueryIdent[{{ $field.Type }}]{Ident: resource.NewIdent[{{ $field.Type }}]("{{ $field.Name }}", p.partialClause)}
}
{{- end }}
{{ end }}

type {{ .Resource.Name }}QueryClause struct {
	clause resource.QueryClause
}

func (qc {{ .Resource.Name }}QueryClause) And() {{ .Resource.Name }}QueryPartialClause {
	return {{ .Resource.Name }}QueryPartialClause{partialClause: qc.clause.And()}
}

func (qc {{ .Resource.Name }}QueryClause) Or() {{ .Resource.Name }}QueryPartialClause {
	return {{ .Resource.Name }}QueryPartialClause{partialClause: qc.clause.Or()}
}

type {{ .Resource.Name }}QueryIdent[T comparable] struct {
	resource.Ident[T]
}

func (i {{ .Resource.Name }}QueryIdent[T]) Equal(v ...T) {{ .Resource.Name }}QueryClause {
	return {{ .Resource.Name }}QueryClause{clause: i.Ident.Equal(v...)}
}

func (i {{ .Resource.Name }}QueryIdent[T]) NotEqual(v ...T) {{ .Resource.Name }}QueryClause {
	return {{ .Resource.Name }}QueryClause{clause: i.Ident.NotEqual(v...)}
}

func (i {{ .Resource.Name }}QueryIdent[T]) GreaterThan(v T) {{ .Resource.Name }}QueryClause {
	return {{ .Resource.Name }}QueryClause{clause: i.Ident.GreaterThan(v)}
}

func (i {{ .Resource.Name }}QueryIdent[T]) GreaterThanEq(v T) {{ .Resource.Name }}QueryClause {
	return {{ .Resource.Name }}QueryClause{clause: i.Ident.GreaterThanEq(v)}
}

func (i {{ .Resource.Name }}QueryIdent[T]) LessThan(v T) {{ .Resource.Name }}QueryClause {
	return {{ .Resource.Name }}QueryClause{clause: i.Ident.LessThan(v)}
}

func (i {{ .Resource.Name }}QueryIdent[T]) LessThanEq(v T) {{ .Resource.Name }}QueryClause {
	return {{ .Resource.Name }}QueryClause{clause: i.Ident.LessThanEq(v)}
}
{{- end }}

{{ if eq .Resource.IsView false }}
type {{ .Resource.Name }}CreatePatch struct {
	patchSet *resource.PatchSet[{{ .Resource.Name }}]
}

{{ $PrimaryKeyIsUUID := .Resource.PrimaryKeyIsUUID }}
{{ if and (eq .Resource.HasCompoundPrimaryKey false) (eq $PrimaryKeyIsUUID true) }}
func New{{ .Resource.Name }}CreatePatchFromPatchSet(patchSet *resource.PatchSet[{{ .Resource.Name }}]) (*{{ .Resource.Name }}CreatePatch, error) {
	id, err := ccc.NewUUID()
	if err != nil {
		return nil, errors.Wrap(err, "ccc.NewUUID()")
	}
	
	patchSet.
		SetKey("ID", id).
		SetPatchType(resource.CreatePatchType)
	
	return &{{ .Resource.Name }}CreatePatch{patchSet: patchSet}, nil
}

func New{{ .Resource.Name }}CreatePatch() (*{{ .Resource.Name }}CreatePatch, error) {
	id, err := ccc.NewUUID()
	if err != nil {
		return nil, errors.Wrap(err, "ccc.NewUUID()")
	}
	
	patchSet := resource.NewPatchSet(resource.NewResourceMetadata[{{ .Resource.Name }}]()).
		SetKey("ID", id).
		SetPatchType(resource.CreatePatchType)

	return &{{ .Resource.Name }}CreatePatch{patchSet: patchSet}, nil
}
{{ else }}
func New{{ .Resource.Name }}CreatePatchFromPatchSet(
{{- range $field := .Resource.Fields -}}
{{ if $field.IsPrimaryKey }}{{ GoCamel $field.Name }} {{ $field.Type }},{{ end }}
{{- end }} patchSet *resource.PatchSet[{{ .Resource.Name }}]) *{{ .Resource.Name }}CreatePatch {
	patchSet.
	{{ range $field := .Resource.Fields }}
	{{ if $field.IsPrimaryKey }}
	 	SetKey("{{ $field.Name }}", {{ GoCamel $field.Name }}).
	{{ end }}
	{{ end }}
		SetPatchType(resource.CreatePatchType)
	
	return &{{ .Resource.Name }}CreatePatch{patchSet: patchSet}
}

func New{{ .Resource.Name }}CreatePatch(
{{- range $isNotFirstIteration, $field := .Resource.Fields }}
{{- if $field.IsPrimaryKey }}{{- if $isNotFirstIteration }}, {{ end }}{{ GoCamel $field.Name }} {{ $field.Type }}{{ end }}{{ end }}) *{{ .Resource.Name }}CreatePatch {
	patchSet := resource.NewPatchSet(resource.NewResourceMetadata[{{ .Resource.Name }}]()).
	{{ range $field := .Resource.Fields }}
	{{ if $field.IsPrimaryKey }}
	 	SetKey("{{ $field.Name }}", {{ GoCamel $field.Name }}).
	{{ end }}
	{{ end }}
		SetPatchType(resource.CreatePatchType)

	return &{{ .Resource.Name }}CreatePatch{patchSet: patchSet}
}
{{ end }}

func (p *{{ .Resource.Name }}CreatePatch) PatchSet() *resource.PatchSet[{{ .Resource.Name }}] {
	return p.patchSet
}

` + fieldAccessors(CreatePatch) + `

type {{ .Resource.Name }}UpdatePatch struct {
	patchSet *resource.PatchSet[{{ .Resource.Name }}]
}

func New{{ .Resource.Name }}UpdatePatchFromPatchSet(
{{- range $field := .Resource.Fields -}}
	{{- if $field.IsPrimaryKey -}}
		{{- GoCamel $field.Name }} {{ $field.Type }},
	{{- end -}}
{{- end -}}
patchSet *resource.PatchSet[{{ .Resource.Name }}]) *{{ .Resource.Name }}UpdatePatch {
	patchSet.
	{{ range $field := .Resource.Fields }}
		{{ if $field.IsPrimaryKey }}
		SetKey("{{ $field.Name }}", {{ GoCamel $field.Name }}).
		{{ end }}
	{{ end }}
		SetPatchType(resource.UpdatePatchType)
	
	return &{{ .Resource.Name }}UpdatePatch{patchSet: patchSet}
}

func New{{ .Resource.Name }}UpdatePatch(
{{- range $isNotFirstIteration, $field := .Resource.Fields -}}
	{{- if $field.IsPrimaryKey }}
		{{- if $isNotFirstIteration }}, {{ end -}}
		{{- GoCamel $field.Name }} {{ $field.Type -}}
	{{- end -}}
{{- end }}) *{{ .Resource.Name }}UpdatePatch {
	patchSet := resource.NewPatchSet(resource.NewResourceMetadata[{{ .Resource.Name }}]()).
{{- range $field := .Resource.Fields }}
	{{- if $field.IsPrimaryKey }}
		SetKey("{{ $field.Name }}", {{ GoCamel $field.Name }}).
	{{- end }}
{{- end }}
		SetPatchType(resource.UpdatePatchType)
	
	return &{{ .Resource.Name }}UpdatePatch{patchSet: patchSet}
}

func (p *{{ .Resource.Name }}UpdatePatch) PatchSet() *resource.PatchSet[{{ .Resource.Name }}] {
	return p.patchSet
}

` + fieldAccessors(UpdatePatch) + `

type {{ .Resource.Name }}DeletePatch struct {
	patchSet *resource.PatchSet[{{ .Resource.Name }}]
}

func New{{ .Resource.Name }}DeletePatchFromPatchSet(
{{- range $field := .Resource.Fields -}}
	{{- if $field.IsPrimaryKey -}}
		{{- GoCamel $field.Name }} {{ $field.Type }},
	{{- end -}}
{{- end -}}
patchSet *resource.PatchSet[{{ .Resource.Name }}]) *{{ .Resource.Name }}DeletePatch {
	patchSet.
	{{ range $field := .Resource.Fields }}
		{{ if $field.IsPrimaryKey }}
		SetKey("{{ $field.Name }}", {{ GoCamel $field.Name }}).
		{{ end }}
	{{ end }}
		SetPatchType(resource.DeletePatchType)
	
	return &{{ .Resource.Name }}DeletePatch{patchSet: patchSet}
}

func New{{ .Resource.Name }}DeletePatch(
{{- range $isNotFirstIteration, $field := .Resource.Fields }}
	{{- if $field.IsPrimaryKey -}}
		{{- if $isNotFirstIteration }}, {{ end -}}
		{{- GoCamel $field.Name }} {{ $field.Type -}}
	{{- end -}}
{{- end }}) *{{ .Resource.Name }}DeletePatch {
	patchSet := resource.NewPatchSet(resource.NewResourceMetadata[{{ .Resource.Name }}]()).
{{- range $field := .Resource.Fields }}
		{{- if $field.IsPrimaryKey }}
		SetKey("{{ $field.Name }}", {{ GoCamel $field.Name }}).
		{{- end }}
{{- end }}
		SetPatchType(resource.DeletePatchType)
	
	return &{{ .Resource.Name }}DeletePatch{patchSet: patchSet}
}

func (p *{{ .Resource.Name }}DeletePatch) PatchSet() *resource.PatchSet[{{ .Resource.Name }}] {
	return p.patchSet
}

{{ range $field := .Resource.Fields }}
{{ if $field.IsPrimaryKey }} 
func (p *{{ $field.Parent.Name }}DeletePatch) {{ $field.Name }}() {{ $field.Type }} {
	v, _ := p.patchSet.Key("{{ $field.Name }}").({{ $field.Type }}) 

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

import (
	"context"
	"net/http"
	"time"

	"{{.PackageName}}/app/router"
	"{{.PackageName}}/spanner"
	"{{.PackageName}}/spanner/resources"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"go.opentelemetry.io/otel"
)

{{ .Handlers }}`

	listTemplate = `func (a *App) {{ Pluralize .Resource.Name }}() http.HandlerFunc {
	type {{ GoCamel .Resource.Name }} struct {
		{{- range $field := .Resource.Fields }}
		{{ $field.Name }} {{ $field.Type}} ` + "`{{ $field.JSONTag }} {{ $field.IndexTag }} {{ $field.ListPermTag }} {{ $field.QueryTag }} {{ $field.SearchIndexTags }}`" + `
		{{- end }}
	}

	type response []*{{ GoCamel .Resource.Name }}

	decoder := NewQueryDecoder[resources.{{ .Resource.Name }}, {{ GoCamel .Resource.Name }}](a, accesstypes.List)

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := otel.Tracer(name).Start(r.Context(), "App.{{ Pluralize .Resource.Name }}()")
		defer span.End()

		querySet, err := decoder.Decode(r, a.UserPermissions(r))
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		res := resources.New{{ .Resource.Name }}QueryFromQuerySet(querySet)
		
		var resp response
		for r, err := range res.Query().SpannerList(ctx, a.ReadTxn()) {
			if err != nil {
				return httpio.NewEncoder(w).ClientMessage(ctx, err)
			}
			resp = append(resp, (*{{ GoCamel .Resource.Name }})(r))
		}

		return httpio.NewEncoder(w).Ok(resp)
	})
}`

	readTemplate = `func (a *App) {{ .Resource.Name }}() http.HandlerFunc {
	type response struct {
		{{- range $field := .Resource.Fields }}
		{{ $field.Name }} {{ $field.Type}} ` + "`{{ $field.JSONTag }} {{ $field.UniqueIndexTag }} {{ $field.ReadPermTag }} {{ $field.QueryTag }} {{ $field.SearchIndexTags }}`" + `
		{{- end }}
	}

	decoder := NewQueryDecoder[resources.{{ .Resource.Name }}, response](a, accesstypes.Read)

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := otel.Tracer(name).Start(r.Context(), "App.{{ .Resource.Name }}()")
		defer span.End()

		id := httpio.Param[{{ .Resource.PrimaryKeyType }}](r, router.{{ .Resource.Name }}ID)

		querySet, err := decoder.Decode(r, a.UserPermissions(r))
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		res := resources.New{{ .Resource.Name }}QueryFromQuerySet(querySet).SetID(id)

		row, err := res.Query().SpannerRead(ctx, a.ReadTxn())
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		return httpio.NewEncoder(w).Ok((*response)(row))
	})
}`

	patchTemplate = `func (a *App) Patch{{ Pluralize .Resource.Name }}() http.HandlerFunc {
	type request struct {
		{{- range $field := .Resource.Fields }}
		{{ $field.Name }} {{ $field.Type}} ` + "`{{ $field.JSONTagForPatch }} {{ $field.PatchPermTag }} {{ $field.QueryTag }}`" + `
		{{- end }}
	}
	
	{{ $PrimaryKeyIsUUID := .Resource.PrimaryKeyIsUUID }}
	{{ $PrimaryKeyType := .Resource.PrimaryKeyType }}
	{{- if $PrimaryKeyIsUUID }}
	type response struct {
		IDs []ccc.UUID ` + "`json:\"iDs\"`" + `
	}
	{{- end }}

	decoder := NewDecoder[resources.{{ .Resource.Name }}, request](a, accesstypes.Create, accesstypes.Update, accesstypes.Delete)

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := otel.Tracer(name).Start(r.Context(), "App.Patch{{ Pluralize .Resource.Name }}()")
		defer span.End()

		{{ if $PrimaryKeyIsUUID }}
		var resp response
		{{- end }}
		eventSource := resource.UserEvent(ctx)

		if err := a.ExecuteFunc(ctx, func(ctx context.Context, txn resource.BufferWriter) error {
			{{- if $PrimaryKeyIsUUID }}
			resp = response{}
			{{- end }}
			r, err := resource.CloneRequest(r)
			if err != nil {
				return errors.Wrap(err, "resource.CloneRequest()")
			}

			for op, err := range resource.Operations(r, "/{id}"{{- if eq false $PrimaryKeyIsUUID }}, resource.RequireCreatePath(){{- end }}) {
				if err != nil {
					return errors.Wrap(err, "resource.Operations()")
				}

				patchSet, err := decoder.DecodeOperation(op, a.UserPermissions(r))
				if err != nil {
					return errors.Wrap(err, "decoder.DecodeOperation()")
				}

				switch op.Type {
				case resource.OperationCreate:
					{{- if $PrimaryKeyIsUUID }}
					patch, err := resources.New{{ .Resource.Name }}CreatePatchFromPatchSet(patchSet)
					if err != nil {
						return errors.Wrap(err, "resources.New{{ .Resource.Name }}CreatePatchFromPatchSet()")
					}
					if err := patch.PatchSet().SpannerBuffer(ctx, txn, eventSource); err != nil {
						return errors.Wrap(err, "resources.{{ .Resource.Name }}CreatePatch.SpannerBuffer()")
					}
					resp.IDs = append(resp.IDs, patch.ID())
					{{- else }}
					id := httpio.Param[{{ $PrimaryKeyType }}](op.Req, "id")
					if err := resources.New{{ .Resource.Name }}CreatePatchFromPatchSet(id, patchSet).PatchSet().SpannerBuffer(ctx, txn, eventSource); err != nil {
						return errors.Wrap(err, "resources.{{ .Resource.Name }}CreatePatch.SpannerBuffer()")
					}
					{{- end }}
				case resource.OperationUpdate:
					id := httpio.Param[{{ $PrimaryKeyType }}](op.Req, "id")
					if err := resources.New{{ .Resource.Name }}UpdatePatchFromPatchSet(id, patchSet).PatchSet().SpannerBuffer(ctx, txn, eventSource); err != nil {
						return errors.Wrap(err, "resources.{{ .Resource.Name }}UpdatePatch.SpannerBuffer()")
					}
				case resource.OperationDelete:
					id := httpio.Param[{{ $PrimaryKeyType }}](op.Req, "id")
					if err := resources.New{{ .Resource.Name }}DeletePatchFromPatchSet(id, patchSet).PatchSet().SpannerBuffer(ctx, txn, eventSource); err != nil {
						return errors.Wrap(err, "resources.{{ .Resource.Name }}DeletePatch.SpannerBuffer()")
					}
				}
			}

			return nil
		}); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, spanner.HandleError[resources.{{ .Resource.Name }}](err))
		}

		{{ if $PrimaryKeyIsUUID  }}
		return httpio.NewEncoder(w).Ok(resp)
		{{ else }}
		return httpio.NewEncoder(w).Ok(nil)
		{{- end -}}
	})
}`

	consolidatedPatchTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

package app

import (
	"context"
	"net/http"
	"time"

	"{{.PackageName}}/app/router"
	"{{.PackageName}}/spanner"
	"{{.PackageName}}/spanner/resources"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
	"go.opentelemetry.io/otel"
)

func (a *App) PatchResources() http.HandlerFunc {
	{{- range $resource := .Resources }}
	type {{ GoCamel $resource.Name }}Request struct {
		{{- range $field := .Fields }}
		{{ $field.Name }} {{ $field.Type}} ` + "`{{ $field.JSONTagForPatch }} {{ $field.PatchPermTag }} {{ $field.QueryTag }}`" + `
		{{- end }}
	}
	{{ GoCamel $resource.Name}}Decoder := NewDecoder[resources.{{ $resource.Name }}, {{ GoCamel $resource.Name }}Request](a, accesstypes.Create, accesstypes.Update, accesstypes.Delete)
	{{ end }}

	type response map[string][]ccc.UUID

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx, span := otel.Tracer(name).Start(r.Context(), "App.PatchResources()")
		defer span.End()

		var (
			eventSource = resource.UserEvent(ctx)
			patches []resource.SpannerBuffer
			resp    response
		)

		if err := a.ExecuteFunc(ctx, func(ctx context.Context, txn resource.BufferWriter) error {
			resp = response{}
			r, err := resource.CloneRequest(r)
			if err != nil {
				return errors.Wrap(err, "resource.CloneRequest()")
			}

			for op, err := range resource.Operations(r, "/{resource}/{id}", resource.RequireCreatePath()) {
				if err != nil {
					return httpio.NewEncoder(w).ClientMessage(ctx, err)
				}
				
				switch httpio.Param[string](op.Req, "resource") {
					{{- range $resource := .Resources -}}
					{{- $primaryKeyType := $resource.PrimaryKeyType }}
					case "{{ Kebab (Pluralize $resource.Name) }}":
						patchSet, err := {{ GoCamel $resource.Name}}Decoder.DecodeOperation(op)
						if err != nil {
							return httpio.NewEncoder(w).ClientMessage(ctx, err)
						}

						switch op.Type {
						case resource.OperationCreate:
							{{- if $resource.PrimaryKeyIsUUID }}
							patch, err := resources.New{{ $resource.Name }}CreatePatchFromPatchSet(patchSet)
							if err != nil {
								return httpio.NewEncoder(w).ClientMessage(ctx, err)
							}
							if err := patch.PatchSet().SpannerBuffer(ctx, txn, eventSource); err != nil {
								return errors.Wrap(err, "resources.{{ $resource.Name }}CreatePatch.SpannerBuffer()")
							}
							resp["{{ GoCamel (Pluralize .Name) }}"] = append(resp["{{ GoCamel (Pluralize .Name) }}"], patch.ID())
							{{- else }}
							id := httpio.Param[{{ $primaryKeyType }}](op.Req, "id")
							if err := resources.New{{ $resource.Name }}CreatePatchFromPatchSet(id, patchSet).PatchSet().SpannerBuffer(ctx, txn, eventSource); err != nil {
								return errors.Wrap(err, "resources.{{ $resource.Name }}CreatePatch.SpannerBuffer()")
							}
							{{- end }}
						case resource.OperationUpdate:
							id := httpio.Param[{{ $primaryKeyType }}](op.Req, "id")
							if err := resources.New{{ $resource.Name }}UpdatePatchFromPatchSet(id, patchSet).PatchSet().SpannerBuffer(ctx, txn, eventSource); err != nil {
								return errors.Wrap(err, "resources.{{ $resource.Name }}UpdatePatch.SpannerBuffer()")
							}
						case resource.OperationDelete:
							id := httpio.Param[{{ $primaryKeyType }}](op.Req, "id")
							if err := resources.New{{ $resource.Name }}DeletePatchFromPatchSet(id, patchSet).PatchSet().SpannerBuffer(ctx, txn, eventSource); err != nil {
								return errors.Wrap(err, "resources.{{ $resource.Name }}DeletePatch.SpannerBuffer()")
							}
						}
					{{- end -}}
				}
			}

			return nil
		}); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, spanner.HandleError[resources.{{ $resource.Name }}](err))
		}

		return httpio.NewEncoder(w).Ok(resp)
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

	{{ range $resource := .Resources }}
	RunResourceTestsFor[resources.{{ $resource.Name }}](t)
	{{- end }}
}`

	typescriptPermissionTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
import { Domain, Permission, Resource } from '@cccteam/ccc-lib';
{{- $permissions := .Permissions }}
{{- $resourcePermissions := .ResourcePermissions }}
{{- $resources := .Resources }}
{{- $resourcetags := .ResourceTags }}
{{- $resourcePermMap := .ResourcePermissionsMap }}
{{- $domains := .Domains }}

type Brand<K, T> = K & {
  __brand: T;
};
export type Method = Brand<string, 'Method'>;

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

{{ with $rpcMethods := .RPCMethods -}}
export const Methods = {
{{- range $rpcMethod := $rpcMethods }}
  {{ $rpcMethod.Name }}: '{{ $rpcMethod.Name }}' as Method,
{{- end }}
};
{{ end -}}

{{ range $resource, $tags := $resourcetags }}
export const {{ $resource }} = {
{{- range $_, $tag := $tags }}
  {{ $tag }}: '{{ $tag }}' as Resource,
{{- end }}
};
{{ end }}
type ResourcePermissions = Record<Permission, boolean>;
type PermissionMappings = Record<Resource, ResourcePermissions>;

const Mappings: PermissionMappings = {
  {{- range $resource := $resources }}
  [Resources.{{ $resource }}]: {
    {{- range $perm := $resourcePermissions }}
    [Permissions.{{ $perm }}]: {{ index $resourcePermMap $resource $perm }},
    {{- end }}
  },
    {{- range $tag := index $resourcetags $resource }}
  [{{$resource.ResourceWithTag $tag }}]: {
      {{- range $perm := $resourcePermissions }}
    [Permissions.{{ $perm }}]: {{ index $resourcePermMap ($resource.ResourceWithTag $tag) $perm }},
      {{- end }}
  },
    {{- end }}
  {{- end }}
};

export function requiresPermission(resource: Resource, permission: Permission): boolean {
  return Mappings[resource][permission];
}

{{ with $rpcMethods := .RPCMethods -}}
type MethodPermissions = Record<Permission, boolean>;
type MethodPermissionMappings = Record<Method, MethodPermissions>;

const MethodMappings: MethodPermissionMappings = {
  {{- range $rpcMethod := $rpcMethods }}
  [Methods.{{ $rpcMethod.Name }}]: {
    [Permissions.Execute]: true,
  },
  {{- end }}
};

export function requiresMethodPermission(resource: Method, permission: Permission): boolean {
  return MethodMappings[resource][permission];
}
{{- end }}
`

	typescriptMetadataTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
import { Resource } from '@cccteam/ccc-lib';
import { Link, ResourceMap, ResourceMeta } from '@components/Resource/resources-helpers';
import { Resources } from './resourcePermissions';
{{- $resources := .Resources }}
{{ range $resource := $resources }}
export interface {{ Pluralize $resource.Name }} {
{{- range $field := $resource.Fields }}
  {{ Camel $field.Name }}: {{ $field.TypescriptDataType }};
{{- end }}
}
{{ end }}
{{ $consolidatedRoute := .ConsolidatedRoute -}}
const resourceMap: ResourceMap = {
  {{- range $resource := $resources }}
  [Resources.{{ Pluralize $resource.Name }}]: {
    route: '{{ Kebab (Pluralize $resource.Name) }}',
    {{- if eq $resource.IsConsolidated true }}
    consolidatedRoute: '{{ $consolidatedRoute }}',
    {{- end }}
    fields: [
      {{- range $field := $resource.Fields }}
      { fieldName: '{{ Camel $field.Name }}', 
       {{- if $field.IsPrimaryKey }} primaryKey: { ordinalPosition: {{ $field.KeyOrdinalPosition }} }, 
       {{- end }} displayType: '{{ Lower $field.TypescriptDisplayType }}', required: {{ $field.IsRequired }}, isIndex: {{ $field.IsIndex -}}
      {{- if $field.IsEnumerated }}, enumeratedResource: Resources.{{ $field.ReferencedResource }}{{ end }} },
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
	routesTemplate = `// Code generated by spannergen. DO NOT EDIT.
// Source: {{ .Source }}

package {{ .Package }}

import (
	"net/http"

	"github.com/cccteam/httpio"
	"github.com/go-chi/chi/v5"
)

const (
	{{ range $Struct := .RoutesMap.Resources }}{{ $Struct }}ID httpio.ParamType = "{{ GoCamel $Struct }}ID"
	{{ end }}
)

type GeneratedHandlers interface {
	{{ range $Struct, $Routes := .RoutesMap }}{{ range $Routes }}{{ .HandlerFunc }}() http.HandlerFunc
	{{ end }}
	{{ end -}}
	{{- if eq .HasConsolidatedHandler true }}PatchResources() http.HandlerFunc{{ end }}
}

func generatedRoutes(r chi.Router, h GeneratedHandlers) {
	{{ range $Struct, $Routes := .RoutesMap }}{{ range $Routes }}r.{{ Pascal .Method }}("{{ .Path }}", h.{{ .HandlerFunc }}())
	{{ end }}
	{{ end -}}
	{{- if eq .HasConsolidatedHandler true }}r.Patch("/{{ .RoutePrefix }}/{{ .ConsolidatedRoute }}", h.PatchResources()){{ end }}
}`

	routerTestTemplate = `// Code generated by handlergen. DO NOT EDIT.
// Source: {{ .Source }}

package {{ .Package }}

import (
	"net/http"

	"{{ .PackageName }}/mock/mock_router"
)

type generatedRouterTest struct {
	url string
	method string
	handlerFunc string
	parameters map[string]string
}

func generatedRouteParameters() []string {
	keys := []string {
		{{ range $Struct := .RoutesMap.Resources }}"{{ GoCamel $Struct }}ID",
		{{ end }}
	}

	return keys
}

{{ $routePrefix := .RoutePrefix -}}
func generatedRouterTests() []*generatedRouterTest {
	routerTests := []*generatedRouterTest {
		{{ range $Struct, $Routes := .RoutesMap }}{{ range $route := $Routes }}{
			url: "{{ DetermineTestURL $Struct $routePrefix $route }}", method: {{ MethodToHttpConst $route.Method }},
			handlerFunc: "{{ $route.HandlerFunc }}",
			parameters: {{ DetermineParameters $Struct $route }},
		},
		{{ end }}{{ end }}
		{{- if eq .HasConsolidatedHandler true -}}
		{
			url: "/{{ .RoutePrefix }}/{{ .ConsolidatedRoute }}", method: http.MethodPatch,
			handlerFunc: "PatchResources",
		},
		{{ end }}
	}

	return routerTests
}

func generatedExpectCalls(e *mock_router.MockHandlersMockRecorder, rec *callRecorder) {
	{{ range $Struct, $Routes := .RoutesMap }}{{ range $Routes }}e.{{ .HandlerFunc }}().Times(1).Return(rec.RecordHandlerCall("{{ .HandlerFunc }}"))
	{{ end }}{{- end -}}
	{{- if eq .HasConsolidatedHandler true }}e.PatchResources().Times(1).Return(rec.RecordHandlerCall("PatchResources")){{ end -}}
}`

	rpcHandlerTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

package app

import (
	"net/http"
	"time"

	"{{ .PackageName }}/app/router"
	"{{ .PackageName }}/spanner"
	"{{ .PackageName }}/spanner/resources"
	"{{ .PackageName }}/businesslayer/rpc"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel"
)

func (a *App) {{ .RPCMethod.Name }}() http.HandlerFunc {
	{{- range $type := .RPCMethod.LocalTypes }}
	type {{ Lower $type.UnqualifiedTypeName }} 
	{{- if $type.IsStruct }} struct {
		{{- range $field := $type.ToStructType.Fields }}
		{{ $field.Name }} {{ $field.Type }} ` + "`json:\"{{ Camel $field.Name }}\"`" + `
		{{- end }}
	}
	{{ else }} {{ $type.Type }}
	{{ end }}
	{{ end }}
	type request struct {
		{{- range $field := .RPCMethod.Fields }}
		{{ $field.Name }} {{ if $field.IsLocalType }}{{ Lower $field.UnqualifiedType }}{{ else }}{{ $field.Type }}{{ end }} ` + "`{{ $field.JSONTag }}`" + `
		{{- end }}
	}

	decoder := NewRPCDecoder[{{ .RPCMethod.Type }}, request](a, accesstypes.Execute)

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error { 
		ctx, span := otel.Tracer(name).Start(r.Context(), "App.{{ .RPCMethod.Name }}()")
		defer span.End()

		params, err := decoder.Decode(r, a.UserPermissions(r), accesstypes.Execute)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}
		
		p := &{{ .RPCMethod.Type }}{
			{{- range $field := .RPCMethod.Fields }}
			{{- if not $field.IsIterable }}
			{{ $field.Name }}: params.{{ $field.Name }},
			{{- end -}}
			{{- end }}
		}
		{{- range $field := .RPCMethod.Fields -}}
		{{- if $field.IsIterable }}
		for _, e := range params.{{ $field.Name }} {
			p.{{ $field.Name }} = append(p.{{ $field.Name }}, {{ $field.TypeName }}(e))
		}
		{{- end }}
		{{- end }}

		if err := a.businessLayer.{{ .RPCMethod.Name }}(ctx, p); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, err)
		}

		return httpio.NewEncoder(w).Ok(nil)
	})
}
`

	rpcInterfacesTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

package rpc

import (
	"github.com/cccteam/ccc/resource"
)

type Method interface {
	Method() accesstypes.Resource
{{ FormatRPCInterfaceTypes .Types }}
}`
	businesslayerInterfacesTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

package businesslayer

import (
	"context"

	"{{.PackageName}}/businesslayer/rpc"
)

type RPCBusinessLayer interface {
{{ range $rpcMethod := .RPCMethods -}}
	{{ $rpcMethod.Name }}(ctx context.Context, runner rpc.{{ if $rpcMethod.Implements "DBRunner"}}DBRunner{{else}}TxnRunner{{end}}) error
{{ end }}
}`

	rpcMethodTemplate = `// Code generated by resourcegeneration. DO NOT EDIT.
// Source: {{ .Source }}

package businesslayer

import (
	"context"

	"{{.PackageName}}/businesslayer/rpc"
	"github.com/go-playground/errors/v5"
)

func (c *Client) {{ .RPCMethod.Name }}(ctx context.Context, runner rpc.{{ if .RPCMethod.Implements "DBRunner"}}DBRunner{{else}}TxnRunner{{end}}) error {
	if err := {{ if .RPCMethod.Implements "DBRunner"}}runner.Execute(ctx, c.DB()){{else}}c.DB().Execute(ctx, runner){{end}}; err != nil {
		return errors.Wrap(err, "{{ .RPCMethod.TypeName }}.Execute()")
	}

	return nil
}
`
)

func fieldAccessors(patchType PatchType) string {
	return fmt.Sprintf(`
		{{- range $field := .Resource.Fields }}
		{{ if eq false $field.IsPrimaryKey }}
		func (p *{{ $field.Parent.Name }}%[1]sPatch) Set{{ $field.Name }}(v {{ if $field.IsLocalType }}{{ $field.UnqualifiedType }}{{ else }}{{ $field.Type }}{{ end }}) *{{ $field.Parent.Name }}%[1]sPatch {
			p.patchSet.Set("{{ $field.Name }}", v)

			return p
		}
		{{ end }}

		func (p *{{ $field.Parent.Name }}%[1]sPatch) {{ $field.Name }}() {{ if $field.IsLocalType }}{{ $field.UnqualifiedType }}{{ else }}{{ $field.Type }}{{ end }} {
		{{ if $field.IsPrimaryKey -}} 
			v, _ := p.patchSet.Key("{{ $field.Name }}").({{ if $field.IsLocalType }}{{ $field.UnqualifiedType }}{{ else }}{{ $field.Type }}{{ end }})
		{{ else -}} 
			v, _ := p.patchSet.Get("{{ $field.Name }}").({{ if $field.IsLocalType }}{{ $field.UnqualifiedType }}{{ else }}{{ $field.Type }}{{ end }}) 
		{{ end }}

			return v
		}

		{{ if eq false $field.IsPrimaryKey  }}
		func (p *{{ $field.Parent.Name }}%[1]sPatch) {{ $field.Name }}IsSet() bool {
			return p.patchSet.IsSet("{{ $field.Name }}")
		}
		{{ end }}
		{{ end }}`, string(patchType))
}
