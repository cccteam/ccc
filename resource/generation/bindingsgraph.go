package generation

import (
	"cmp"
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// The bindings graph: every binding a resources package declares, drawn. A
// via: path spells only its remote segments — `@attribute(hangarZone, via:
// Zone)` on HangarID says "Zone" and nothing else — so reading one needs the
// foreign-key schema in the reader's head, and a bare binding's name is
// legible only in the struct that declares it. The generator draws the whole
// vocabulary as one committed, drift-tested DOT file per package, the way it
// draws each workflow: a box per resource declaring an attribute or subject
// binding, listing its bare bindings; the tables paths land on or foreign
// keys point at; the requester where every subject binding starts; one edge
// per hop however many paths share it; and a thin grey edge from a bare
// binding on a foreign key to its table, so the two names a condition pairs
// (assignedSquadron IN subject.squadrons) visibly meet. Facts only: no grant
// or condition is drawn.

// bindingsGraphKind is a binding kind, which sets its edge style.
type bindingsGraphKind int

const (
	// bindingsGraphAttribute is an @attribute(via:) path, drawn solid.
	bindingsGraphAttribute bindingsGraphKind = iota
	// bindingsGraphDomain is an @domain(via:) path, drawn dotted.
	bindingsGraphDomain
	// bindingsGraphSubject is the requester entering an anchor, and a dotted
	// @subjectSet / @subjectValue value continuing from it, drawn dashed.
	bindingsGraphSubject
	// bindingsGraphReference is a bare binding on a foreign key pointing at
	// the table it references, drawn thin and grey.
	bindingsGraphReference
)

// style renders the kind's edge attribute prefix.
func (k bindingsGraphKind) style() string {
	switch k {
	case bindingsGraphDomain:
		return "style=dotted, "
	case bindingsGraphSubject:
		return "style=dashed, "
	case bindingsGraphReference:
		return "color=gray55, fontcolor=gray35, arrowhead=vee, arrowsize=0.6, "
	default:
		return ""
	}
}

// bindingsGraphSubjectNode is the requester's node, where every subject binding starts.
const bindingsGraphSubjectNode = "subject"

// bindingsGraphEdge is one drawn hop or reference. Label carries the binding
// name and, on a hop, the column it leaves through and the column read where
// the path terminates, so two paths sharing a hop under one name are one edge.
type bindingsGraphEdge struct {
	Source string
	Target string
	Kind   bindingsGraphKind
	Label  string
}

// bindingsGraphNode is a resource declaring a binding: its name and the bare
// bindings it lists, one `name: column` line each, subject ones prefixed
// `subject.`. A resource whose bindings are all paths lists nothing.
type bindingsGraphNode struct {
	Name     string
	Bindings []string
}

// bindingsGraph is one package's assembled graph: the resources declaring a
// binding, the tables a path lands on or a foreign key points at, whether the
// requester is drawn, and the deduplicated edges, every list sorted so the
// rendered file is byte-stable across runs.
type bindingsGraph struct {
	Declaring []bindingsGraphNode
	Landed    []string
	Subject   bool
	Edges     []bindingsGraphEdge
}

// bindingsGraphBuilder accumulates one package's graph facts, resource by
// resource, before the sorted graph is taken from it.
type bindingsGraphBuilder struct {
	// structName resolves a table to the struct backing it where one is
	// parsed, the table name otherwise, as the workflow graph does.
	structName func(table string) string
	// declaring maps each declaring resource to the bare-binding lines its
	// box lists; a resource whose bindings are all paths maps to none.
	declaring map[string][]string
	landed    map[string]bool
	edges     map[bindingsGraphEdge]bool
	subject   bool
}

// newBindingsGraphBuilder prepares the table-to-struct resolution over the
// parsed resources.
func newBindingsGraphBuilder(c *client) *bindingsGraphBuilder {
	byTable := make(map[string]*resourceInfo, len(c.resources))
	for _, res := range c.resources {
		byTable[c.pluralize(res.Name())] = res
	}

	return &bindingsGraphBuilder{
		structName: func(table string) string {
			if res, ok := byTable[table]; ok {
				return res.Name()
			}

			return table
		},
		declaring: make(map[string][]string),
		landed:    make(map[string]bool),
		edges:     make(map[bindingsGraphEdge]bool),
	}
}

// declare marks a resource as declaring; a non-empty line is one bare binding
// its box lists.
func (b *bindingsGraphBuilder) declare(name, line string) {
	lines, ok := b.declaring[name]
	if line != "" {
		lines = append(lines, line)
	}
	if !ok || line != "" {
		b.declaring[name] = lines
	}
}

// walk draws one path leaving source through column: each hop is an edge into
// the hop's table labeled with the column left through, and the last carries
// the column the path reads.
func (b *bindingsGraphBuilder) walk(source, column string, path []bindingHop, kind bindingsGraphKind, name string) {
	b.declare(source, "")
	for i, hop := range path {
		target := b.structName(hop.Table)
		label := name + ": " + column
		if i == len(path)-1 {
			label += " ⇒ " + hop.Column
		}
		b.edges[bindingsGraphEdge{Source: source, Target: target, Kind: kind, Label: label}] = true
		b.landed[target] = true
		source, column = target, hop.Column
	}
}

// refer draws a bare binding on a foreign key pointing at its table; the state
// column's enum table stays undrawn, the workflow graph covers the states.
func (b *bindingsGraphBuilder) refer(source string, field *resourceField, name string) {
	if !field.IsForeignKey || field.ReferencedResource == "" || field.IsState {
		return
	}
	target := b.structName(field.ReferencedResource)
	b.edges[bindingsGraphEdge{Source: source, Target: target, Kind: bindingsGraphReference, Label: name}] = true
	b.landed[target] = true
}

// addResource draws one resource's bindings: every attribute (a path hop by
// hop, a bare one listed and, on a foreign key, pointed at its table), the
// domain binding as a path only (the bare tenant column is never a condition
// operand), and the subject vocabulary.
func (b *bindingsGraphBuilder) addResource(res *resourceInfo) {
	for _, attr := range res.Attributes {
		if len(attr.Path) > 0 {
			b.walk(res.Name(), fieldColumn(attr.Anchor), attr.Path, bindingsGraphAttribute, attr.Name)

			continue
		}
		b.declare(res.Name(), attr.Name+": "+fieldColumn(attr.Anchor))
		b.refer(res.Name(), attr.Anchor, attr.Name)
	}
	if domain := res.DomainBinding; domain != nil && len(domain.Path) > 0 {
		b.walk(res.Name(), fieldColumn(domain.Anchor), domain.Path, bindingsGraphDomain, domainKeyword)
	}
	b.addSubjects(res)
}

// addSubjects draws one resource's subject sets and values: a dotted value as
// a path, a bare one listed as subject.name and pointed at its table when on
// a foreign key, and the requester entering each anchor column once, naming
// every set and value it yields there.
func (b *bindingsGraphBuilder) addSubjects(res *resourceInfo) {
	namesByAnchor := make(map[string][]string)
	for _, subject := range slices.Concat(res.SubjectSets, res.SubjectValues) {
		b.subject = true
		anchor := fieldColumn(subject.Anchor)
		namesByAnchor[anchor] = append(namesByAnchor[anchor], subject.Name)
		if len(subject.Path) > 0 {
			b.walk(res.Name(), fieldColumn(subject.ValueField), subject.Path, bindingsGraphSubject, subject.Name)

			continue
		}
		qualified := bindingsGraphSubjectNode + "." + subject.Name
		b.declare(res.Name(), qualified+": "+fieldColumn(subject.ValueField))
		b.refer(res.Name(), subject.ValueField, qualified)
	}
	for anchor, names := range namesByAnchor {
		slices.Sort(names)
		b.edges[bindingsGraphEdge{
			Source: bindingsGraphSubjectNode,
			Target: res.Name(),
			Kind:   bindingsGraphSubject,
			Label:  strings.Join(names, ", ") + ": " + anchor,
		}] = true
	}
}

// graph takes the assembled facts as the sorted graph: declaring resources by
// name with their lines sorted, landed tables minus the declaring ones, and
// edges by kind, source, target, label.
func (b *bindingsGraphBuilder) graph() *bindingsGraph {
	graph := &bindingsGraph{Subject: b.subject}
	for name, lines := range b.declaring {
		slices.Sort(lines)
		graph.Declaring = append(graph.Declaring, bindingsGraphNode{Name: name, Bindings: lines})
	}
	slices.SortFunc(graph.Declaring, func(x, y bindingsGraphNode) int {
		return strings.Compare(x.Name, y.Name)
	})
	for name := range b.landed {
		if _, ok := b.declaring[name]; !ok {
			graph.Landed = append(graph.Landed, name)
		}
	}
	slices.Sort(graph.Landed)
	for edge := range b.edges {
		graph.Edges = append(graph.Edges, edge)
	}
	slices.SortFunc(graph.Edges, func(x, y bindingsGraphEdge) int {
		return cmp.Or(
			cmp.Compare(x.Kind, y.Kind),
			strings.Compare(x.Source, y.Source),
			strings.Compare(x.Target, y.Target),
			strings.Compare(x.Label, y.Label),
		)
	})

	return graph
}

// assembleBindingsGraph builds the graph from the parsed resources' compiled
// bindings, every list sorted so the rendered file is byte-stable across runs.
func (c *client) assembleBindingsGraph() *bindingsGraph {
	builder := newBindingsGraphBuilder(c)
	for _, res := range c.resources {
		builder.addResource(res)
	}

	return builder.graph()
}

// bindingsGraphLabel renders a declaring resource's box as an HTML-like
// Graphviz label: the name in bold over one left-aligned line per bare binding.
func bindingsGraphLabel(node bindingsGraphNode) string {
	var b strings.Builder
	b.WriteString(`<table border="0" cellborder="0" cellspacing="0" cellpadding="1">`)
	fmt.Fprintf(&b, `<tr><td align="left"><b>%s</b></td></tr>`, html.EscapeString(node.Name))
	for _, line := range node.Bindings {
		fmt.Fprintf(&b, `<tr><td align="left">%s</td></tr>`, html.EscapeString(line))
	}
	b.WriteString(`</table>`)

	return b.String()
}

// renderBindingsDOT draws the graph: a box per declaring resource listing its
// bare bindings, dashed boxes for the tables landed on or pointed at, the
// requester as an ellipse, one labeled edge per hop or reference in the
// kind's style, and a legend. Layout stays Graphviz's.
func renderBindingsDOT(graph *bindingsGraph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by resourcegeneration. DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Bindings: every attribute and subject binding this package declares. A box per\n")
	fmt.Fprintf(&b, "// declaring resource lists its bare bindings as `name: column`; dashed boxes are\n")
	fmt.Fprintf(&b, "// the tables a path lands on or a foreign key points at; the subject ellipse is\n")
	fmt.Fprintf(&b, "// the requester, entering each anchor once with every name it yields there. One\n")
	fmt.Fprintf(&b, "// edge per hop, drawn once however many paths share it: solid for @attribute,\n")
	fmt.Fprintf(&b, "// dotted for @domain, dashed for a subject binding, labeled `name: column` on a\n")
	fmt.Fprintf(&b, "// continuing hop and `name: column ⇒ column` where the path terminates; a thin\n")
	fmt.Fprintf(&b, "// grey edge points a bare binding on a foreign key at its table. Facts only: no\n")
	fmt.Fprintf(&b, "// grant or condition is drawn.\n")
	fmt.Fprintf(&b, "digraph Bindings {\n")
	fmt.Fprintf(&b, "\trankdir=LR;\n")
	fmt.Fprintf(&b, "\tnode [shape=box];\n")
	for _, node := range graph.Declaring {
		if len(node.Bindings) == 0 {
			fmt.Fprintf(&b, "\t%q;\n", node.Name)

			continue
		}
		fmt.Fprintf(&b, "\t%q [label=<%s>];\n", node.Name, bindingsGraphLabel(node))
	}
	for _, name := range graph.Landed {
		fmt.Fprintf(&b, "\t%q [style=dashed];\n", name)
	}
	if graph.Subject {
		fmt.Fprintf(&b, "\t%q [shape=ellipse];\n", bindingsGraphSubjectNode)
	}
	for _, edge := range graph.Edges {
		fmt.Fprintf(&b, "\t%q -> %q [%slabel=%q];\n", edge.Source, edge.Target, edge.Kind.style(), edge.Label)
	}
	fmt.Fprintf(&b, "\tsubgraph cluster_legend {\n")
	fmt.Fprintf(&b, "\t\tlabel=\"legend\";\n")
	fmt.Fprintf(&b, "\t\t\"declares a binding\" [shape=box];\n")
	fmt.Fprintf(&b, "\t\t\"landed on / pointed at\" [shape=box, style=dashed];\n")
	fmt.Fprintf(&b, "\t\t\"requester\" [shape=ellipse];\n")
	legend := []struct {
		id    string
		kind  bindingsGraphKind
		label string
	}{
		{id: attributeKeyword, kind: bindingsGraphAttribute, label: "@" + attributeKeyword + " path"},
		{id: domainKeyword, kind: bindingsGraphDomain, label: "@" + domainKeyword + " path"},
		{id: bindingsGraphSubjectNode, kind: bindingsGraphSubject, label: "@" + subjectSetKeyword + " / @" + subjectValueKeyword},
		{id: "reference", kind: bindingsGraphReference, label: "bare binding on a foreign key"},
	}
	for _, entry := range legend {
		fmt.Fprintf(&b, "\t\t\"legend:%s\" [shape=point];\n", entry.id)
		fmt.Fprintf(&b, "\t\t\"legend:%s:to\" [shape=point];\n", entry.id)
	}
	for _, entry := range legend {
		fmt.Fprintf(&b, "\t\t\"legend:%s\" -> \"legend:%s:to\" [%slabel=%q];\n", entry.id, entry.id, entry.kind.style(), entry.label)
	}
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "}\n")

	return b.String()
}

// generateBindingsGraph emits the package's bindings graph into the resources
// package as zz_gen_bindings.dot, beside the workflow graphs. A package
// declaring no attribute or subject binding writes no file; the
// generated-file sweep has already removed a stale one.
func (r *resourceGenerator) generateBindingsGraph() error {
	graph := r.assembleBindingsGraph()
	if len(graph.Declaring) == 0 {
		return nil
	}

	destination := filepath.Join(r.resource.Dir(), genPrefix+"_bindings.dot")
	if err := os.WriteFile(destination, []byte(renderBindingsDOT(graph)), 0o644); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): file: %s", destination)
	}
	log.Printf("Generated bindings graph: %v\n", destination)

	return nil
}

// generateGraphs emits the DOT review surfaces into the resources package: one
// workflow graph per stateful root, then the package's bindings graph. Both
// draw declared transitions or synthesized state paths, so they render only
// after RPC extraction and workflow resolution.
func (r *resourceGenerator) generateGraphs() error {
	if err := r.generateWorkflowGraphs(); err != nil {
		return errors.Wrap(err, "resourceGenerator.generateWorkflowGraphs()")
	}
	if err := r.generateBindingsGraph(); err != nil {
		return errors.Wrap(err, "resourceGenerator.generateBindingsGraph()")
	}

	return nil
}
