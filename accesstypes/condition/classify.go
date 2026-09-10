package condition

import (
	"slices"
)

// RowFree reports whether the expression references no row data — no binding
// names and no new. prefix; only subject, subject attributes, now, and
// literals. The classification is purely lexical: subject, subject.*, and
// now are reserved, and every other identifier is a binding name.
//
// Row-free is the class permitted on surfaces where no row exists (scope-wide,
// RPC, and computed-resource grants); row-referencing conditions there are
// rejected at load and by MigrateRoles.
func RowFree(e Expr) bool {
	rowFree := true
	walk(e, func(node Expr) {
		switch n := node.(type) {
		case Comparison:
			if !n.Left.IsNow() && !n.Left.IsTemporal() {
				rowFree = false
			}
		case In:
			// The left side is a binding — now IN is rejected at parse — except
			// a temporal function (dayOfWeek IN days), which is a fact.
			if !n.Left.IsTemporal() {
				rowFree = false
			}
		case NullTest:
			rowFree = false
		}
	})

	return rowFree
}

// Bindings returns the distinct binding names the expression references in
// attribute positions — now excluded, pre- and post-image collapsed to the
// name, a comparison's right-side attribute (the old-vs-new form) included —
// sorted lexically. MigrateRoles validates these against the Collection's
// bindings.
func Bindings(e Expr) []string {
	set := map[string]struct{}{}
	walk(e, func(node Expr) {
		var ref Ref
		switch n := node.(type) {
		case Comparison:
			ref = n.Left
			if right, ok := n.Right.(Ref); ok {
				set[right.Name] = struct{}{}
			}
		case In:
			ref = n.Left
		case NullTest:
			ref = n.Left
		default:
			return
		}
		if !ref.IsNow() && !ref.IsTemporal() {
			set[ref.Name] = struct{}{}
		}
	})

	return sortedKeys(set)
}

// ComparesAttributes reports whether any comparison's right side is an
// attribute reference — the old-vs-new form, `new.attr <op> attr`, which
// relates the post-image to the pre-image of the same row. Only an update has
// both images, so MigrateRoles rejects the form on every other permission.
func ComparesAttributes(e Expr) bool {
	found := false
	walk(e, func(node Expr) {
		if n, ok := node.(Comparison); ok {
			if _, ok := n.Right.(Ref); ok {
				found = true
			}
		}
	})

	return found
}

// SubjectSets returns the distinct @subjectSet names the expression
// references (`attr IN subject.name`), sorted lexically.
func SubjectSets(e Expr) []string {
	set := map[string]struct{}{}
	walk(e, func(node Expr) {
		if n, ok := node.(In); ok && n.SubjectSet != "" {
			set[n.SubjectSet] = struct{}{}
		}
	})

	return sortedKeys(set)
}

// SubjectValues returns the distinct @subjectValue names the expression
// references as scalar operands (`subject.name`), sorted lexically.
func SubjectValues(e Expr) []string {
	set := map[string]struct{}{}
	walk(e, func(node Expr) {
		n, ok := node.(Comparison)
		if !ok {
			return
		}
		if v, ok := n.Right.(SubjectValue); ok {
			set[v.Name] = struct{}{}
		}
	})

	return sortedKeys(set)
}

// UsesNow reports whether the expression references the environment fact now —
// on either side of a comparison, or through a temporal function of it — the
// check must then carry an Environment with the instant, and its absence is a
// fail-loud error.
func UsesNow(e Expr) bool {
	uses := false
	walk(e, func(node Expr) {
		switch n := node.(type) {
		case Comparison:
			if n.Left.IsNow() || n.Left.IsTemporal() {
				uses = true
			}
			if _, ok := n.Right.(Now); ok {
				uses = true
			}
		case In:
			if n.Left.IsTemporal() {
				uses = true
			}
		}
	})

	return uses
}

// UsesPostImage reports whether the expression reads the post-write overlay
// (`new.`). MigrateRoles enforces image validity per permission: Update takes
// both images, Read and Delete are unqualified only, and Insert's single
// image is written unqualified.
func UsesPostImage(e Expr) bool {
	uses := false
	walk(e, func(node Expr) {
		var ref Ref
		switch n := node.(type) {
		case Comparison:
			ref = n.Left
		case In:
			ref = n.Left
		case NullTest:
			ref = n.Left
		default:
			return
		}
		if ref.PostImage {
			uses = true
		}
	})

	return uses
}

// walk visits every node of the tree, parents before children.
func walk(e Expr, visit func(Expr)) {
	visit(e)
	switch n := e.(type) {
	case And:
		for _, op := range n.Operands {
			walk(op, visit)
		}
	case Or:
		for _, op := range n.Operands {
			walk(op, visit)
		}
	case Not:
		walk(n.Operand, visit)
	}
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}
