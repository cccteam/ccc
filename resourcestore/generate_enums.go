package resourcestore

import (
	"iter"
	"maps"
	"os"
	"slices"
	"text/template"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

const enumTemplate = `
{{- range .}}
export enum {{.Name}} {
{{- range .Values}}
  {{.}} = '{{.}}',
{{- end}}
}
{{end}}
`

type Enum struct {
	Name   string
	Values []string
}

func (s *Store) GenerateTypeScriptEnums(dst string) error {
	perms := make(map[accesstypes.Permission]struct{})
	resources := make(map[accesstypes.Resource]struct{})
	for _, store := range s.resourceStore {
		for resource, fMap := range store {
			for _, perm := range fMap {
				perms[perm] = struct{}{}
				resources[resource] = struct{}{}
			}
		}
	}

	fields := make(map[accesstypes.Resource]map[accesstypes.Tag]struct{})
	for _, store := range s.tagStore {
		for r, resourceFields := range store {
			for field, permissions := range resourceFields {
				if fields[r] == nil {
					fields[r] = make(map[accesstypes.Tag]struct{})
				}
				fields[r][field] = struct{}{}
				for _, perm := range permissions {
					perms[perm] = struct{}{}
				}
			}
		}
	}

	enums := []Enum{{
		Name:   "Permissions",
		Values: collectSortedStrings(maps.Keys(perms)),
	}, {
		Name:   "Resources",
		Values: collectSortedStrings(maps.Keys(resources)),
	}}

	for _, field := range collectSortedStrings(maps.Keys(fields)) {
		fields := fields[accesstypes.Resource(field)]
		enums = append(enums, Enum{
			Name:   field,
			Values: collectSortedStrings(maps.Keys(fields)),
		})
	}

	if err := writeFile(dst, enums); err != nil {
		return err
	}

	return nil
}

func writeFile(dst string, enums []Enum) error {
	f, err := os.Create(dst)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer f.Close()

	if _, err := f.WriteString("// This file is auto-generated. Do not edit manually."); err != nil {
		return errors.Wrap(err, "f.WriteString()")
	}

	t := template.Must(template.New("typescript enums").Parse(enumTemplate))
	if err := t.Execute(f, enums); err != nil {
		return errors.Wrap(err, "t.Execute()")
	}

	if err := f.Close(); err != nil {
		return errors.Wrap(err, "f.Close()")
	}

	return nil
}

func collectSortedStrings[T iter.Seq[S], S ~string](seq T) (s []string) {
	for elem := range seq {
		s = append(s, string(elem))
	}

	slices.Sort(s)

	return s
}
