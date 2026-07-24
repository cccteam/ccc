package resource

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
)

// CollectionDiff describes, for one resource and scope, the registrations present on
// only one side of a runtime-vs-generated collection comparison. RuntimeOnly groups
// carry what the runtime registered beyond the generated collection; the inverse
// groups carry what the generated collection declares beyond the runtime.
//
// Permissions are resource-level differences — the actionable kind: registering or
// declaring the permission's Set typically resolves the group's field-level
// differences with it. Tags, TagPermissions, and ImmutableTags are field-level
// differences that only stand on their own when the two sides agree on the permission
// but disagree on a request struct's shape.
type CollectionDiff struct {
	Resource    accesstypes.Resource
	Scope       accesstypes.PermissionScope
	RuntimeOnly bool

	Permissions []accesstypes.Permission
	// Tags lists tags registered on this side only; their per-tag permissions are
	// implied missing with them and are not repeated in TagPermissions.
	Tags []accesstypes.Tag
	// TagPermissions lists, for tags both sides register, the per-tag permissions
	// present on this side only.
	TagPermissions map[accesstypes.Tag][]accesstypes.Permission
	ImmutableTags  []accesstypes.Tag

	// ManualRegistrations records which Permissions entries arrived through the
	// deprecated Collection.AddResource, so callers can name the exact declaration
	// that resolves them.
	ManualRegistrations []ManualRegistration
}

// FieldDiffCount returns the number of field-level differences in the group: tags,
// per-tag permissions, and immutable markers.
func (d *CollectionDiff) FieldDiffCount() int {
	n := len(d.Tags) + len(d.ImmutableTags)
	for _, perms := range d.TagPermissions {
		n += len(perms)
	}

	return n
}

// String renders the group compactly for logs and test failures.
func (d CollectionDiff) String() string {
	direction := "runtime-only"
	if !d.RuntimeOnly {
		direction = "generated-only"
	}

	parts := make([]string, 0, 4)
	if len(d.Permissions) > 0 {
		parts = append(parts, fmt.Sprintf("permissions %v", d.Permissions))
	}
	if len(d.Tags) > 0 {
		parts = append(parts, fmt.Sprintf("tags %v", d.Tags))
	}
	if len(d.TagPermissions) > 0 {
		parts = append(parts, fmt.Sprintf("tag permissions %v", d.TagPermissions))
	}
	if len(d.ImmutableTags) > 0 {
		parts = append(parts, fmt.Sprintf("immutable tags %v", d.ImmutableTags))
	}

	return fmt.Sprintf("%s: resource %q scope %q %s", direction, d.Resource, d.Scope, strings.Join(parts, ", "))
}

// Annotation renders the @manualAddResource annotation that declares the registration
// to the generator.
func (m ManualRegistration) Annotation() string {
	if m.Scope != accesstypes.GlobalPermissionScope && m.Scope != "" {
		return fmt.Sprintf("@manualAddResource(%s, %s)", m.Permission, m.Scope)
	}

	return fmt.Sprintf("@manualAddResource(%s)", m.Permission)
}

// DiffCollections compares a runtime-populated Collection against a
// GeneratedCollection and returns one CollectionDiff per (resource, scope, direction)
// with differences, sorted by scope, resource, and direction (runtime-only first); an
// empty slice means the two are equivalent. Comparison is set-based: registration
// order and duplicate manual registrations do not produce differences.
func DiffCollections(runtime *Collection, generated *GeneratedCollection) []CollectionDiff {
	runtime.mu.RLock()
	runtimeData := collectionDataFrom(runtime)
	manual := make(map[ManualRegistration]struct{}, len(runtime.manualRegistrations))
	for registration := range runtime.manualRegistrations {
		manual[registration] = struct{}{}
	}
	runtime.mu.RUnlock()

	generatedData := generated.Data()

	diffs := oneWayDiffs(runtimeData, generatedData, true, manual)
	diffs = append(diffs, oneWayDiffs(generatedData, runtimeData, false, nil)...)

	slices.SortFunc(diffs, func(a, b CollectionDiff) int {
		if c := strings.Compare(string(a.Scope), string(b.Scope)); c != 0 {
			return c
		}
		if c := strings.Compare(string(a.Resource), string(b.Resource)); c != 0 {
			return c
		}
		if a.RuntimeOnly == b.RuntimeOnly {
			return 0
		}
		if a.RuntimeOnly {
			return -1
		}

		return 1
	})

	return diffs
}

// oneWayDiffs returns one CollectionDiff per resource in from that carries
// registrations absent from to. For runtime-side diffs, manual carries the
// registrations recorded through the deprecated Collection.AddResource so their
// provenance can be attached.
func oneWayDiffs(from, to CollectionData, runtimeOnly bool, manual map[ManualRegistration]struct{}) []CollectionDiff {
	type resourceKey struct {
		scope accesstypes.PermissionScope
		name  accesstypes.Resource
	}

	toIndex := make(map[resourceKey]CollectionResource, len(to.Resources))
	for _, res := range to.Resources {
		toIndex[resourceKey{scope: res.Scope, name: res.Name}] = res
	}

	var diffs []CollectionDiff
	for _, res := range from.Resources {
		other := toIndex[resourceKey{scope: res.Scope, name: res.Name}]
		diff := CollectionDiff{Resource: res.Name, Scope: res.Scope, RuntimeOnly: runtimeOnly}

		for _, perm := range res.Permissions {
			if slices.Contains(other.Permissions, perm) {
				continue
			}
			diff.Permissions = append(diff.Permissions, perm)

			registration := ManualRegistration{Scope: res.Scope, Permission: perm, Resource: res.Name}
			if _, ok := manual[registration]; ok {
				diff.ManualRegistrations = append(diff.ManualRegistrations, registration)
			}
		}

		otherTags := make(map[accesstypes.Tag]TagData, len(other.Tags))
		for _, tag := range other.Tags {
			otherTags[tag.Name] = tag
		}
		for _, tag := range res.Tags {
			otherTag, ok := otherTags[tag.Name]
			if !ok {
				diff.Tags = append(diff.Tags, tag.Name)

				continue
			}

			for _, perm := range tag.Permissions {
				if slices.Contains(otherTag.Permissions, perm) {
					continue
				}
				if diff.TagPermissions == nil {
					diff.TagPermissions = make(map[accesstypes.Tag][]accesstypes.Permission)
				}
				diff.TagPermissions[tag.Name] = append(diff.TagPermissions[tag.Name], perm)
			}
		}

		for _, tag := range res.ImmutableTags {
			if !slices.Contains(other.ImmutableTags, tag) {
				diff.ImmutableTags = append(diff.ImmutableTags, tag)
			}
		}

		if len(diff.Permissions) == 0 && diff.FieldDiffCount() == 0 {
			continue
		}

		slices.Sort(diff.Permissions)
		slices.Sort(diff.Tags)
		slices.Sort(diff.ImmutableTags)
		for _, perms := range diff.TagPermissions {
			slices.Sort(perms)
		}
		slices.SortFunc(diff.ManualRegistrations, func(a, b ManualRegistration) int {
			return strings.Compare(string(a.Permission), string(b.Permission))
		})
		diffs = append(diffs, diff)
	}

	return diffs
}
