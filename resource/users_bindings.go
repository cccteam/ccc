package resource

import (
	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// The condition vocabulary of the user surface: the attributes a grant's condition may
// name on each resource, with the comparison types deploy validation checks literals
// against, and the application-wide subject bindings the caller's own row gives
// (subject.ownOrganization, subject.ownRoles). The names are the rows' wire names.
const (
	userAttrUsername               = "username"
	userAttrDisabled               = "disabled"
	userAttrHasPassword            = "hasPassword"
	userAttrPasswordChangeRequired = "passwordChangeRequired"
	userAttrCreatedAt              = "createdAt"
	userAttrLastSignInAt           = "lastSignInAt"
	userAttrOrganization           = "organization"
	userAttrRolesSynced            = "rolesSynced"

	seatAttrUser        = "user"
	seatAttrUsername    = "username"
	seatAttrRole        = "role"
	seatAttrEveryDomain = "everyDomain"
	seatAttrEverywhere  = "everywhere"

	roleAttrName = "name"
	roleAttrKind = "kind"

	// UserSubjectOrganization and UserSubjectRoles are the subject bindings the surface
	// declares once per application: the caller's own organization
	// (subject.ownOrganization) and the roles the caller holds in the partition
	// (subject.ownRoles), read off the caller's row in the Auth whose surface serves the
	// request. A resource's attributes and subject bindings share one namespace, so the
	// caller's facts are named apart from the row's (organization = subject.ownOrganization).
	UserSubjectOrganization = "ownOrganization"
	UserSubjectRoles        = "ownRoles"
)

// column names an attribute of a composed row: a binding with no path, so a condition
// may read it in the post-image too.
func column(name string, typ accesstypes.AttributeType) AttributeData {
	return AttributeData{Name: name, Column: name, Type: typ}
}

// userBindings is the users resources' vocabulary; withSubjects adds the application-wide
// subject bindings, declared on one resource of the application since subject.<name> is
// one namespace.
func userBindings(withSubjects bool) *Bindings {
	b := &Bindings{
		Attributes: []AttributeData{
			column(userAttrUsername, accesstypes.AttributeTypeString),
			column(userAttrDisabled, accesstypes.AttributeTypeBool),
			column(userAttrHasPassword, accesstypes.AttributeTypeBool),
			column(userAttrPasswordChangeRequired, accesstypes.AttributeTypeBool),
			column(userAttrCreatedAt, accesstypes.AttributeTypeTimestamp),
			column(userAttrLastSignInAt, accesstypes.AttributeTypeTimestamp),
			column(userAttrOrganization, accesstypes.AttributeTypeString),
			column(userAttrRolesSynced, accesstypes.AttributeTypeBool),
		},
	}
	if withSubjects {
		b.SubjectValues = []SubjectBindingData{{Name: UserSubjectOrganization, UserColumn: userAttrUsername, Column: userAttrOrganization, Type: accesstypes.AttributeTypeString}}
		b.SubjectSets = []SubjectBindingData{{Name: UserSubjectRoles, UserColumn: userAttrUsername, Column: seatAttrRole, Type: accesstypes.AttributeTypeString}}
	}

	return b
}

// seatBindings is the seats resources' vocabulary.
func seatBindings(tenanted bool) *Bindings {
	flag := seatAttrEveryDomain
	if tenanted {
		flag = seatAttrEverywhere
	}

	return &Bindings{Attributes: []AttributeData{
		column(seatAttrUser, accesstypes.AttributeTypeString),
		column(seatAttrUsername, accesstypes.AttributeTypeString),
		column(seatAttrRole, accesstypes.AttributeTypeString),
		column(flag, accesstypes.AttributeTypeBool),
	}}
}

// roleBindings is the roles resources' vocabulary.
func roleBindings(tenanted bool) *Bindings {
	attrs := []AttributeData{column(roleAttrName, accesstypes.AttributeTypeString)}
	if !tenanted {
		attrs = append(attrs, column(roleAttrKind, accesstypes.AttributeTypeString))
	}

	return &Bindings{Attributes: attrs}
}

// RegisterUserSurface registers one Auth's surface into the collection: each partition's
// three resources with their field registrations, orders and vocabulary, and the methods
// that target its users. withSubjects declares the subject bindings on this Auth's
// global users resource; the generator sets it on the first Auth alone. The generator
// calls it for every Auth whose outlet the generated router serves.
func RegisterUserSurface(b *CollectionBuilder, decl *UserSurfaceDeclaration, withSubjects bool) error {
	if err := decl.Validate(); err != nil {
		return err
	}
	names := decl.Names()
	if err := registerUserPartition(b, accesstypes.GlobalPermissionScope, &names.Global, decl.Shape, withSubjects); err != nil {
		return err
	}
	if names.Tenant.Tenanted {
		if err := registerUserPartition(b, accesstypes.DomainPermissionScope, &names.Tenant, decl.Shape, false); err != nil {
			return err
		}
	}

	return nil
}

// registerUserPartition registers one partition.
func registerUserPartition(b *CollectionBuilder, scope accesstypes.PermissionScope, n *UserPartitionNames, shape UserSurfaceShape, withSubjects bool) error {
	readUsers := fieldTagsOf[userRequest]()
	writeUsers := fieldTagsOf[userWriteRequest]()
	readSeats := fieldTagsOf[seatRequest]()
	readRoles := fieldTagsOf[roleRequest]()
	if n.Tenanted {
		writeUsers = fieldTagsOf[tenantUserWriteRequest]()
		readSeats = fieldTagsOf[tenantSeatRequest]()
		readRoles = fieldTagsOf[tenantRoleRequest]()
	}
	userWrites := []accesstypes.Permission{accesstypes.Update, accesstypes.Delete}
	if shape.CreatesUsers() {
		userWrites = append(userWrites, accesstypes.Create)
	}

	if err := registerUserResource(b, scope, n.Users, readUsers, writeUsers, userWrites, []accesstypes.Tag{userAttrUsername}, userBindings(withSubjects)); err != nil {
		return err
	}
	if err := registerUserResource(b, scope, n.Seats, readSeats, fieldTagsOf[seatWriteRequest](), []accesstypes.Permission{accesstypes.Create, accesstypes.Delete}, []accesstypes.Tag{seatAttrUsername, seatAttrRole}, seatBindings(n.Tenanted)); err != nil {
		return err
	}
	if err := registerUserResource(b, scope, n.Roles, readRoles, nil, nil, []accesstypes.Tag{roleAttrName}, roleBindings(n.Tenanted)); err != nil {
		return err
	}
	for _, method := range n.ServedMethods(shape) {
		if err := b.AddMethodResource(scope, accesstypes.Execute, method); err != nil {
			return errors.Wrapf(err, "registering %s", method)
		}
		b.SetMethodTarget(scope, method, n.Users)
	}

	return nil
}

// registerUserResource registers one resource: its read fields under List and Read, its
// write fields under the write permissions it takes, its order and its vocabulary.
func registerUserResource(b *CollectionBuilder, scope accesstypes.PermissionScope, res accesstypes.Resource, read, write []FieldTags, writes []accesstypes.Permission, order []accesstypes.Tag, bindings *Bindings) error {
	for _, perm := range []accesstypes.Permission{accesstypes.List, accesstypes.Read} {
		set, err := NewSetData(read, perm)
		if err != nil {
			return errors.Wrap(err, "resource.NewSetData()")
		}
		if err := b.AddResourceSet(scope, res, set); err != nil {
			return errors.Wrapf(err, "registering %s %s", res, perm)
		}
	}
	for _, perm := range writes {
		// Delete is enforced at the resource level: its registration carries no fields.
		fields := write
		if perm == accesstypes.Delete {
			fields = nil
		}
		set, err := NewSetData(fields, perm)
		if err != nil {
			return errors.Wrap(err, "resource.NewSetData()")
		}
		if err := b.AddResourceSet(scope, res, set); err != nil {
			return errors.Wrapf(err, "registering %s %s", res, perm)
		}
	}
	b.SetResourceQueryKeys(scope, res, order, nil)
	b.SetResourceBindings(scope, res, bindings)

	return nil
}

// ServedMethods lists the methods the shape serves in the partition: the password
// reset where the Auth has a password method, the unlinking where it has registrations
// to link identities through.
func (n *UserPartitionNames) ServedMethods(shape UserSurfaceShape) []accesstypes.Resource {
	var methods []accesstypes.Resource
	if shape.Password {
		methods = append(methods, n.SetPassword)
	}
	if len(shape.Registrations) > 0 {
		methods = append(methods, n.UnlinkIdentity)
	}

	return methods
}
