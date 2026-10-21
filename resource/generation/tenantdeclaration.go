package generation

import (
	"go/types"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// The tenant record's declaration of who may enter a tenant (signin.TenantDeclaration),
// kept as data of the application rather than deploy configuration: the record's
// directory-only column (@directoryOnly, a BOOL saying the tenant's people enter
// through its directories only, so a password does not reach it) and the directories
// record (@tenantDirectory), the tenant-scoped table whose rows are the directories a
// tenant admits its people through, one per declaration, each naming the sign-in
// method (@directoryMethod: google, azure, workos, as sessioninfo.AuthMethod spells
// them) and the connection the method's identities carry (@directoryConnection: a
// Google Workspace by its domain, a Microsoft Entra tenant by its tenant id). The
// generator holds the shape at capture, so the generated constructors
// (New<Record>Roster with the declaration's columns, New<Record>DeclarationReader for
// the sign-in policy) and the write paths that keep every instance's copy current
// render against a declaration that can serve as one.

// resolveTenantDeclaration reads a table-backed struct's declaration annotations:
// @tenantDirectory on the struct, @directoryOnly, @directoryMethod and
// @directoryConnection on its fields, each refused where it cannot mean anything.
// The rules that read across structs (one directories record, the record it belongs
// to) run once every struct is extracted (rejectSecondTenantDirectory,
// validateTenantDeclaration). The scope and the tenant record resolve first.
func resolveTenantDeclaration(res *resourceInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	if annotations.Struct.Has(tenantDirectoryKeyword) {
		switch {
		case res.IsTenant:
			return errors.Newf("struct %s: @%s on the tenant record; the directories record is a tenant-scoped resource of its own, one row per directory a tenant admits its people through", res.Name(), tenantDirectoryKeyword)
		case !res.IsDomainScoped():
			return errors.Newf("struct %s: @%s on a global resource; the directories record is tenant-scoped (@%s(%s)), since a tenant's directories are its own and its administrator declares them", res.Name(), tenantDirectoryKeyword, permissionScopeKeyword, "domain")
		}
		res.IsTenantDirectory = true
	}
	for i, field := range pStruct.Fields() {
		fieldAnnotations := annotations.Fields[i]
		switch {
		case fieldAnnotations.Has(directoryOnlyKeyword):
			if !res.IsTenant {
				return errors.Newf("struct %s field %s: @%s on a resource that is not the tenant record; the directory-only flag is the tenant record's column, so annotate the @%s record's BOOL column", res.Name(), field.Name(), directoryOnlyKeyword, tenantKeyword)
			}
			if !isBoolKinded(field.GoType()) {
				return errors.Newf("struct %s field %s: @%s on a %s column; the directory-only flag is a BOOL, true when the tenant's people enter through its directories only", res.Name(), field.Name(), directoryOnlyKeyword, field.Type())
			}
			if res.DirectoryOnlyField != nil {
				return errors.Newf("struct %s field %s: @%s on a second column; field %s carries it already, and a tenant has one directory-only flag", res.Name(), field.Name(), directoryOnlyKeyword, res.DirectoryOnlyField.Name())
			}
			res.DirectoryOnlyField = res.fieldNamed(field.Name())
		case fieldAnnotations.Has(directoryMethodKeyword):
			if err := res.setDirectoryColumn(&res.DirectoryMethodField, field, directoryMethodKeyword, "the sign-in method a directory is reached through"); err != nil {
				return err
			}
		case fieldAnnotations.Has(directoryConnectionKeyword):
			if err := res.setDirectoryColumn(&res.DirectoryConnectionField, field, directoryConnectionKeyword, "the connection a directory's identities carry"); err != nil {
				return err
			}
		}
	}

	return nil
}

// setDirectoryColumn records one of the directories record's two columns, refusing it
// off the record, on a column that is not a string, and declared twice.
func (r *resourceInfo) setDirectoryColumn(dest **resourceField, field *parser.Field, keyword, meaning string) error {
	switch {
	case !r.IsTenantDirectory:
		return errors.Newf("struct %s field %s: @%s on a resource that is not the directories record; %s is the @%s record's column, so annotate the record's struct @%s first", r.Name(), field.Name(), keyword, meaning, tenantDirectoryKeyword, tenantDirectoryKeyword)
	case !isStringKinded(field.GoType()):
		return errors.Newf("struct %s field %s: @%s on a %s column; %s is a string", r.Name(), field.Name(), keyword, field.Type(), meaning)
	case *dest != nil:
		return errors.Newf("struct %s field %s: @%s on a second column; field %s carries it already", r.Name(), field.Name(), keyword, (*dest).Name())
	}
	*dest = r.fieldNamed(field.Name())
	if *dest == nil {
		return errors.Newf("struct %s field %s: @%s on a column the schema does not hold", r.Name(), field.Name(), keyword)
	}

	return nil
}

// fieldNamed returns the resource's field by its Go name, nil when the struct's field
// was refused at capture (a spanner tag naming no column).
func (r *resourceInfo) fieldNamed(name string) *resourceField {
	for _, f := range r.Fields {
		if f.Name() == name {
			return f
		}
	}

	return nil
}

// isBoolKinded reports whether the type's underlying type is bool.
func isBoolKinded(t types.Type) bool {
	if t == nil {
		return false
	}
	basic, ok := t.Underlying().(*types.Basic)

	return ok && basic.Kind() == types.Bool
}

// SignalsTenants reports whether the resource's generated write paths keep every
// instance's tenant roster current after their commit: the tenant record, whose rows
// are the tenants and whose directory-only flag is part of what each declares, and the
// directories record, whose rows are the rest of the declaration.
func (r *resourceInfo) SignalsTenants() bool {
	return r.IsTenant || r.IsTenantDirectory
}

// HasDeclaration reports whether the tenant record declares who may enter a tenant:
// a directories record is declared in its package (set by validateTenantDeclaration).
func (r *resourceInfo) HasDeclaration() bool {
	return r.declaration != nil
}

// tenantDeclaration is what the tenant record's package declares about who may enter a
// tenant, resolved once every struct is extracted: the directories record with its
// two columns, and the record's directory-only column when it carries one.
type tenantDeclaration struct {
	Directories *resourceInfo
}

// rejectSecondTenantDirectory refuses a second @tenantDirectory in the package: a
// tenant record has one directories record, since its declaration is one thing.
func rejectSecondTenantDirectory(resources []*resourceInfo) error {
	var first *resourceInfo
	for _, res := range resources {
		if !res.IsTenantDirectory {
			continue
		}
		if first != nil {
			return errors.Newf("struct %s: @%s on a second struct; struct %s is the directories record already, and a tenant record has one", res.Name(), tenantDirectoryKeyword, first.Name())
		}
		first = res
	}

	return nil
}

// tenantDirectoryRecord returns the package's directories record, nil when none is
// declared.
func (r *resourceGenerator) tenantDirectoryRecord() *resourceInfo {
	for _, res := range r.resources {
		if res.IsTenantDirectory {
			return res
		}
	}

	return nil
}

// validateTenantDeclaration holds the package's declaration to its shape once the
// tenant record and the bindings are known: the directories record belongs to the
// tenant record through the bare @domain column (the tenant's key on the row itself,
// never a join path), carries both of its columns, and the record's directory-only
// flag needs the directories record, since a tenant whose people enter through its
// directories only needs directories to enter through. With the declaration whole the
// record carries it, so the generated constructors and write paths read it.
func (r *resourceGenerator) validateTenantDeclaration() error {
	record := r.tenantRecord()
	directories := r.tenantDirectoryRecord()
	if directories == nil {
		if record != nil && record.DirectoryOnlyField != nil {
			return errors.Newf("struct %s field %s: @%s with no directories record in the package; a tenant whose people enter through its directories only needs directories to enter through, so declare the directories record (@%s) or drop the flag", record.Name(), record.DirectoryOnlyField.Name(), directoryOnlyKeyword, tenantDirectoryKeyword)
		}

		return nil
	}
	if record == nil {
		// Unreachable past resolveTenantRecord, which refuses a tenant-scoped resource
		// with no record; kept so the rule reads whole.
		return errors.Newf("struct %s: %s", directories.Name(), tenantNeeded)
	}
	if directories.DomainBinding == nil || len(directories.DomainBinding.Path) > 0 {
		return errors.Newf("struct %s: @%s needs the bare @%s column, the tenant's key on the row itself, since a directory is the tenant's own declaration; a join path (@%s(via: ...)) is refused", directories.Name(), tenantDirectoryKeyword, domainKeyword, domainKeyword)
	}
	if directories.DirectoryMethodField == nil {
		return errors.Newf("struct %s: @%s with no @%s column; annotate the string column holding the sign-in method a directory is reached through (google, azure, workos)", directories.Name(), tenantDirectoryKeyword, directoryMethodKeyword)
	}
	if directories.DirectoryConnectionField == nil {
		return errors.Newf("struct %s: @%s with no @%s column; annotate the string column holding the connection a directory's identities carry (a Google Workspace by its domain, a Microsoft Entra tenant by its tenant id)", directories.Name(), tenantDirectoryKeyword, directoryConnectionKeyword)
	}
	record.declaration = &tenantDeclaration{Directories: directories}

	return nil
}

// tenantDeclarationColumns is the declaration as the generated constructors hand it to
// resource.WithTenantDeclarations and resource.NewTenantDeclarationReader: the column
// names behind the record's annotated fields.
type tenantDeclarationColumns struct {
	DirectoryOnly    string
	DirectoriesTable string
	TenantColumn     string
	MethodColumn     string
	ConnectionColumn string
}

// declarationColumns reads the declaration's columns off the record, nil when the
// record declares none.
func (r *resourceGenerator) declarationColumns(record *resourceInfo) *tenantDeclarationColumns {
	if !record.HasDeclaration() {
		return nil
	}
	directories := record.declaration.Directories
	columns := &tenantDeclarationColumns{
		DirectoriesTable: r.pluralize(directories.Name()),
		TenantColumn:     spannerColumn(directories.DomainBinding.Anchor),
		MethodColumn:     spannerColumn(directories.DirectoryMethodField),
		ConnectionColumn: spannerColumn(directories.DirectoryConnectionField),
	}
	if record.DirectoryOnlyField != nil {
		columns.DirectoryOnly = spannerColumn(record.DirectoryOnlyField)
	}

	return columns
}

// spannerColumn is the column a field maps to, as its spanner tag names it.
func spannerColumn(field *resourceField) string {
	column, _ := field.LookupTag(spannerTagKey)

	return column
}
