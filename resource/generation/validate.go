package generation

import (
	"path/filepath"
	"strings"

	"github.com/cccteam/ccc/resource/generation/parser"
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

		if caser.ToSnake(sName) != strings.TrimSuffix(fileName, ".go") {
			return errors.Newf("%s (%s) does not match its file name %s (expected %q)", s.Name(), caser.ToSnake(sName), fileName, caser.ToSnake(sName)+".go")
		}

		return nil
	}
}
