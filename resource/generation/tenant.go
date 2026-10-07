package generation

import (
	"go/types"
	"log"
	"path/filepath"
	"time"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

// The tenant record (@tenant) is the global, table-backed resource whose rows are the
// tenants: its route name is the segment tenant-scoped routes are served under, its
// single key is the domain in every tenant-scoped URL, and its generated write paths
// keep the application's TenantRoster current. The rules below are checked where the
// declaration is captured, each refusal naming its rule, so nothing renders against a
// record that cannot serve as one.

// resolveTenant applies a table-backed struct's @tenant: the record is global, carries
// one key, and that key is a string, the slug every tenant-scoped URL carries and the
// generated write paths hand the roster as the domain. The scope must be resolved first.
func resolveTenant(res *resourceInfo, annotations genlang.StructAnnotations) error {
	if !annotations.Struct.Has(tenantKeyword) {
		return nil
	}
	if res.IsDomainScoped() {
		return errors.Newf("struct %s: @%s on a tenant-scoped resource; the tenant record is global, since its rows are the tenants themselves", res.Name(), tenantKeyword)
	}
	if res.HasCompoundPrimaryKey() {
		return errors.Newf("struct %s: @%s on a resource with a compound primary key; the tenant record has one key, the domain in every tenant-scoped URL", res.Name(), tenantKeyword)
	}
	key := res.PrimaryKey()
	if key == nil {
		return errors.Newf("struct %s: @%s on a resource with no primary key; the tenant record has one key, the domain in every tenant-scoped URL", res.Name(), tenantKeyword)
	}
	if !isStringKinded(key.GoType()) {
		return errors.Newf("struct %s: @%s on a resource whose key %s is %s; the tenant record's key is a string, the domain in every tenant-scoped URL", res.Name(), tenantKeyword, key.Name(), key.Type())
	}
	res.IsTenant = true

	return nil
}

// isStringKinded reports whether the type's underlying type is string, so a value of it
// converts to accesstypes.Domain.
func isStringKinded(t types.Type) bool {
	if t == nil {
		return false
	}
	basic, ok := t.Underlying().(*types.Basic)

	return ok && basic.Kind() == types.String
}

// rejectTenantAnnotation refuses @tenant on a struct of another kind: a view, a
// computed resource and a method have no table whose rows could be the tenants.
func rejectTenantAnnotation(pStruct *parser.Struct, annotations genlang.StructAnnotations, kind string) error {
	if !annotations.Struct.Has(tenantKeyword) {
		return nil
	}

	return errors.Newf("struct %s: @%s on a %s; the tenant record is a table-backed @%s, since its rows are the tenants", pStruct.Name(), tenantKeyword, kind, resourceKeyword)
}

// rejectReservedStem refuses a struct whose plural file stem is one the generator
// writes for itself (reservedOutputStems), naming the file and what the generator
// writes to it, so the collision is refused at capture rather than written over. The
// tenant record is where it was first met: a record named Tenant has the handler file
// zz_gen_tenants.go, which the roster constructor's file once shared. Called for every
// resource kind through rejectReservedResourceName.
func rejectReservedStem(name, stem string) error {
	output, reserved := reservedOutputStems[stem]
	if !reserved {
		return nil
	}

	return errors.Newf("struct %s: its generated files would take the stem of %s, the file the generator writes %s to; rename the resource", name, output.file, output.carries)
}

// rejectSecondTenant refuses a second @tenant in the package: an application has one
// tenant record, since one route segment serves its tenant-scoped routes.
func rejectSecondTenant(resources []*resourceInfo) error {
	var first *resourceInfo
	for _, res := range resources {
		if !res.IsTenant {
			continue
		}
		if first != nil {
			return errors.Newf("struct %s: @%s on a second struct; struct %s is the tenant record already, and an application has one", res.Name(), tenantKeyword, first.Name())
		}
		first = res
	}

	return nil
}

// tenantRecord returns the package's tenant record, nil when none is declared.
func (r *resourceGenerator) tenantRecord() *resourceInfo {
	for _, res := range r.resources {
		if res.IsTenant {
			return res
		}
	}

	return nil
}

// tenantNeeded is the sentence a tenant-scoped declaration hears when the package
// declares no tenant record.
const tenantNeeded = "a tenant-scoped resource needs a tenant record; annotate it @" + tenantKeyword

// resolveTenantRecord runs once the table, view and computed kinds are extracted: a
// tenant-scoped resource, view or computed resource needs the tenant record, since its
// routes are served under the record's segment and its domain is checked against the
// record's roster, so a package with any and no @tenant is refused naming each. With the
// record known, the domain route segment is its route name and the domain route
// parameter its key's route parameter (ToGoCamel(name+key)), the one chi wildcard name
// the record's read route and the segment pair share at that tree position. The RPC
// methods are extracted later and checked by requireTenantRecordForMethods.
func (r *resourceGenerator) resolveTenantRecord() error {
	tenant := r.tenantRecord()
	if tenant == nil {
		var errs []error
		for _, res := range r.resources {
			if res.IsDomainScoped() {
				errs = append(errs, errors.Newf("struct %s: %s", res.Name(), tenantNeeded))
			}
		}
		for _, res := range r.computedResources {
			if res.IsDomainScoped() {
				errs = append(errs, errors.Newf("struct %s: %s", res.Name(), tenantNeeded))
			}
		}
		if len(errs) > 0 {
			return errors.Wrap(errors.Join(errs...), "tenant record error")
		}

		return nil
	}

	r.domainRouteSegment = strcase.ToKebab(r.pluralize(tenant.Name()))
	r.domainRouteParam = strcase.ToGoCamel(tenant.Name() + tenant.PrimaryKey().Name())

	return nil
}

// requireTenantRecordForMethods is resolveTenantRecord's rule for the RPC methods, which
// are extracted after the record resolved: a tenant-scoped method in a package with no
// tenant record is refused naming it.
func (r *resourceGenerator) requireTenantRecordForMethods() error {
	if r.tenantRecord() != nil {
		return nil
	}
	var errs []error
	for _, method := range r.rpcMethods {
		if method.IsDomainScoped() {
			errs = append(errs, errors.Newf("struct %s: %s", method.Name(), tenantNeeded))
		}
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "tenant record error")
	}

	return nil
}

// tenantsData feeds the handler package's tenants file: the record's roster constructor.
type tenantsData struct {
	Source  string
	Package string
	// Name is the record's struct name; Table and KeyColumn are what the constructor
	// hands resource.NewTenantRoster.
	Name      string
	Table     string
	KeyColumn string
}

// generateTenants emits the tenant record's roster constructor, New<Record>Roster, into
// the handler package whenever the package declares a tenant record.
func (r *resourceGenerator) generateTenants() error {
	tenant := r.tenantRecord()
	if tenant == nil {
		return nil
	}

	begin := time.Now()
	destinationFilePath := filepath.Join(r.handler.Dir(), generatedGoFileName(tenantRosterOutputName))

	keyColumn, _ := columnTag(tenant.PrimaryKey())
	if err := r.writeFormattedGoFile(destinationFilePath, "tenantsTemplate", tenantsTemplate, &tenantsData{
		Source:    r.resource.Dir(),
		Package:   r.handler.Package(),
		Name:      tenant.Name(),
		Table:     r.pluralize(tenant.Name()),
		KeyColumn: keyColumn,
	}); err != nil {
		return errors.Wrap(err, "writeFormattedGoFile()")
	}
	log.Printf("Generated tenants file in %s: %s", time.Since(begin), destinationFilePath)

	return nil
}
