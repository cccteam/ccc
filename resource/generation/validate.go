package generation

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

type structValidator func(*parser.Struct) error

func validate(s *parser.Struct, validators ...structValidator) error {
	var errs []error
	for _, validate := range validators {
		if err := validate(s); err != nil {
			errs = append(errs, err)

			continue
		}
	}

	if len(errs) != 0 {
		return errors.Wrap(errors.Join(errs...), "validation error")
	}

	return nil
}

// validateNoPermTags rejects perm struct tags on source structs: field permissions are
// enforced structurally from the endpoint permission, so the tag is dead annotation
// (the generator emits the perm:"-" primary-key marker into request structs itself).
func validateNoPermTags(s *parser.Struct) error {
	var errs []error
	for _, field := range s.Fields() {
		if field.HasTag(permTagKey) {
			errs = append(errs, errors.Newf("field %s.%s carries a perm tag: field permissions are derived structurally from the endpoint permission; remove the tag", s.Name(), field.Name()))
		}
	}

	if len(errs) != 0 {
		return errors.Wrap(errors.Join(errs...), "perm tag error")
	}

	return nil
}

func (c *client) validateStructNameMatchesFile(pkg *packages.Package, plural bool) structValidator {
	return func(s *parser.Struct) error {
		fileName := filepath.Base(pkg.Fset.Position(s.Pos()).Filename)

		sName := s.Name()
		if plural {
			sName = c.pluralize(sName)
		}

		expected := fileStem(sName)
		if expected != strings.TrimSuffix(fileName, ".go") {
			if strings.HasSuffix(expected, "_test"+testFileMarker) {
				return errors.Newf("%s (%s) does not match its file name %s (expected %q: the name ends in Test, so the file carries the %s marker to keep it, and the generated files beside it, out of Go's _test.go files)", s.Name(), expected, fileName, expected+".go", testFileMarker)
			}

			return errors.Newf("%s (%s) does not match its file name %s (expected %q)", s.Name(), expected, fileName, expected+".go")
		}

		return nil
	}
}

// validateConditionsTags rejects a conditions tag value the generator does not
// recognize. Every reader of the tag matches its values exactly (IsPII, IsImmutable,
// IsOutputOnly, IsInputOnly), so a misspelled or space-padded value would silently drop
// the condition it meant to declare; refusing it names the value and suggests the
// nearest recognized one, the way the scanner does for a misspelled keyword.
func validateConditionsTags(s *parser.Struct) error {
	var errs []error
	for _, field := range s.Fields() {
		tag, ok := field.LookupTag(conditionsTagKey)
		if !ok {
			continue
		}
		for value := range strings.SplitSeq(tag, ",") {
			if slices.Contains(conditionValues, value) {
				continue
			}
			hint := ""
			if suggestion, ok := suggestCondition(value); ok {
				hint = "; did you mean \"" + suggestion + "\"?"
			}
			errs = append(errs, errors.Newf("field %s.%s: conditions tag value %q is not recognized%s (recognized: %s)", s.Name(), field.Name(), value, hint, strings.Join(conditionValues, ", ")))
		}
	}

	if len(errs) != 0 {
		return errors.Wrap(errors.Join(errs...), "conditions tag error")
	}

	return nil
}

// suggestCondition names the recognized value an unrecognized one was likely meant to
// be: the value itself once padding is removed (a short word shifted by a space is too
// far for the similarity measure), else the nearest by the scanner's own measure.
func suggestCondition(value string) (string, bool) {
	if trimmed := strings.TrimSpace(value); trimmed != value && slices.Contains(conditionValues, trimmed) {
		return trimmed, true
	}

	return genlang.Suggest(value, conditionValues)
}
