package generation

import (
	"fmt"
	"slices"
)

// Finding is an advisory finding the audit pass raises about a shape the framework
// handles under a stated limitation: a normal generation never prints it, since it
// would fire on every run of every application that carries the shape, and a runner
// prints it on demand after a successful run (Generator.Audit, the -audit flag of an
// application's generate program). Every Finding is one of the kinds in this file.
type Finding interface {
	fmt.Stringer
	finding()
}

// CascadeReleaseFinding says a resource stores files on a table whose rows the
// database deletes by cascade, so those rows never pass through the patch machinery,
// the release of their objects (README section 13) never runs for them, and the
// application's sweep removes the objects later. Parent is set when the table is an
// interleaved child declared ON DELETE CASCADE; Column when a foreign key on the
// table carries the CASCADE delete rule. One finding per cause.
type CascadeReleaseFinding struct {
	Resource string
	Table    string
	Parent   string
	Column   string
}

func (CascadeReleaseFinding) finding() {}

// String renders the one-line finding, names verbatim from the schema.
func (f CascadeReleaseFinding) String() string {
	cause := "interleaved in " + f.Parent
	if f.Column != "" {
		cause = "through the foreign key on " + f.Column
	}

	return fmt.Sprintf("%s stores files on %s, whose rows the database deletes by cascade, %s, so a cascade releases none of their objects and the sweep removes them; an application that cares deletes the rows by patch first (README section 13)",
		f.Resource, f.Table, cause)
}

// auditFindings reads the extracted table-backed resources that store files against
// the table map for the shapes the audit pass reports: today, a table whose rows the
// database deletes by cascade (cascadeReleaseFindings). A virtual resource has no
// table (a view's rows are the tables' own), a computed resource has none at all, and
// a resource without a @file has nothing to release. Findings come in resource-name
// order, each resource's causes in their own order.
func (c *client) auditFindings(resources []*resourceInfo) []Finding {
	sorted := slices.Clone(resources)
	sortResources(sorted)

	var findings []Finding
	for _, res := range sorted {
		if res.IsVirtual || len(res.Files) == 0 {
			continue
		}
		table := c.pluralize(res.Name())
		meta, ok := c.tableMap[table]
		if !ok {
			continue
		}
		findings = append(findings, cascadeReleaseFindings(res.Name(), table, meta)...)
	}

	return findings
}

// cascadeReleaseFindings reads one file-storing resource's table for the cascade
// causes: the interleave first, when the table is a child declared ON DELETE CASCADE,
// then each foreign-key column carrying the CASCADE delete rule, in ordinal order.
func cascadeReleaseFindings(resourceName, table string, meta *tableMetadata) []Finding {
	var findings []Finding
	if meta.OnDeleteCascade {
		findings = append(findings, CascadeReleaseFinding{Resource: resourceName, Table: table, Parent: meta.ParentTable})
	}

	columns := make([]string, 0, len(meta.Columns))
	for name, column := range meta.Columns {
		if column.IsForeignKey && column.DeleteRule == cascadeDeleteAction {
			columns = append(columns, name)
		}
	}
	slices.SortFunc(columns, func(a, b string) int {
		return int(meta.Columns[a].OrdinalPosition - meta.Columns[b].OrdinalPosition)
	})
	for _, column := range columns {
		findings = append(findings, CascadeReleaseFinding{Resource: resourceName, Table: table, Column: column})
	}

	return findings
}
