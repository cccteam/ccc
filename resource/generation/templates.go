package generation

import "fmt"

var (
	resourcesInterfaceTemplate = `// Code generated by spannergen. DO NOT EDIT.
// Source: {{ .Source }}

package resources

import (
	"github.com/cccteam/ccc/resource"
)

type Resource interface {
	resource.Resourcer
	{{ range $i, $e := .Types }}{{ if $i }} | {{ end }}{{ .Name }}{{ end }}
}`

	resourceFileTemplate = `// Code generated by spannergen. DO NOT EDIT.
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
func (q *{{ $TypeName }}Query) SetKey{{ .Name }}(v {{ .Type }}) *{{ $TypeName }}Query {
	q.qSet.SetKey("{{ .Name }}", v)

	return q
}

func (q *{{ $TypeName }}Query) {{ .Name }}() {{ .Type }} {
	v, _ := q.qSet.Key("{{ .Name }}").({{ .Type }})

	return v
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
{{ if and (eq .IsCompoundTable false) (eq $PrimaryKeyIsUUID true) }}
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

func (p *{{ .Name }}CreatePatch) InsertPatchSet() *resource.PatchSet[{{ .Name }}] {
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
{{- if eq .IsPrimaryKey true }}{{ GoCamel .Name }} {{ .Type }}{{ end }}{{ end }}) *{{ .Name }}UpdatePatch {
	patchSet := resource.NewPatchSet(resource.NewResourceMetadata[{{ .Name }}]()).
	{{- range .Fields }}
	{{- if eq .IsPrimaryKey true }}
		SetKey("{{ .Name }}", {{ GoCamel .Name }}).
	{{- end }}
	{{- end }}
		SetPatchType(resource.UpdatePatchType)
	
	return &{{ .Name }}UpdatePatch{patchSet: patchSet}
}

func (p *{{ .Name }}UpdatePatch) UpdatePatchSet() *resource.PatchSet[{{ .Name }}] {
	return p.patchSet
}

` + fieldAccessors(UpdatePatch) + `

type {{ .Name }}DeletePatch struct {
	patchSet *resource.PatchSet[{{ .Name }}]
}

func New{{ .Name }}DeletePatch(
{{- range $i, $e := .Fields }}
{{- if eq .IsPrimaryKey true }}{{ GoCamel .Name }} {{ .Type}}{{ end }}{{ end }}) *{{ .Name }}DeletePatch {
	patchSet := resource.NewPatchSet(resource.NewResourceMetadata[{{ .Name }}]()).
	{{- range .Fields }}
	{{- if eq .IsPrimaryKey true }}
		SetKey("{{ .Name }}", {{ GoCamel .Name }}).
	{{- end }}
	{{- end }}
		SetPatchType(resource.DeletePatchType)
	
	return &{{ .Name }}DeletePatch{patchSet: patchSet}
}

func (p *{{ .Name }}DeletePatch) DeletePatchSet() *resource.PatchSet[{{ .Name }}] {
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
	listTemplate = `func (a *App) {{ Pluralize .Type.Name }}() http.HandlerFunc {
	type {{ GoCamel .Type.Name }} struct {
		{{ range .Type.Fields }}{{ .Name }} {{ .Type }} ` + "`json:\"{{ DetermineJSONTag . false }}\"{{ FormatPerm .ListPerm }}{{ FormatQueryTag .QueryTag }}`" + `
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
	type response struct {
		{{ range .Type.Fields }}{{ .Name }} {{ .Type }} ` + "`json:\"{{ DetermineJSONTag . false }}\"{{ FormatPerm .ReadPerm }}{{ FormatQueryTag .QueryTag }}`" + `
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

		row, err := spanner.Read(ctx, a.businessLayer.DB(), resources.New{{ .Type.Name }}QueryFromQuerySet(querySet).SetKeyID(id))
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

		for op, err := range resource.Operations(r, "/{id}") {
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
				patches = append(patches, patch.InsertPatchSet())
				resp.IDs = append(resp.IDs, patch.ID())
				{{- else }}
				id := httpio.Param[{{ $PrimaryKeyType }}](op.Req, "id")
				patches = append(patches, resources.New{{ .Type.Name }}CreatePatchFromPatchSet(id, patchSet).InsertPatchSet())
				{{- end }}
			case resource.OperationUpdate:
				id := httpio.Param[{{ $PrimaryKeyType }}](op.Req, "id")
				patches = append(patches, resources.New{{ .Type.Name }}UpdatePatchFromPatchSet(id, patchSet).UpdatePatchSet())
			case resource.OperationDelete:
				id := httpio.Param[{{ $PrimaryKeyType }}](op.Req, "id")
				patches = append(patches, resources.New{{ .Type.Name }}DeletePatch(id).DeletePatchSet())
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
)

func fieldAccessors(patchType PatchType) string {
	return fmt.Sprintf(`{{ $TypeName := .Name}}
		{{ range .Fields }}
		{{ if eq .IsPrimaryKey false }}
		func (p *{{ $TypeName }}%[1]sPatch) Set{{ .Name }}(v {{ TrimPtr .Type }}) *{{ $TypeName }}%[1]sPatch {
			{{- $IsPtr := IsPtr .Type }}
			{{- if eq $IsPtr true }}
			p.patchSet.Set("{{ .Name }}", &v)
			{{- else }}
			p.patchSet.Set("{{ .Name }}", v)
			{{ end}}

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
