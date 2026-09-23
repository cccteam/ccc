package generation

import (
	"cmp"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// The bindings graph: every join-path binding a resources package declares,
// drawn. A via: path spells only its remote segments — `@attribute(hangarZone,
// via: Zone)` on HangarID says "Zone" and nothing else — so reading one needs
// the foreign-key schema in the reader's head, and the resolved hops are
// otherwise visible only as the collection's Path literals. The generator
// draws them as one committed, drift-tested DOT file per package, the way it
// draws each workflow: the resources declaring a path, the tables paths land
// on, the requester where a subject path starts, one edge per hop however
// many paths share it. Facts only: no grant or condition is drawn.

// bindingsGraphKind is a binding kind, which sets its edge style.
type bindingsGraphKind int

const (
	// bindingsGraphAttribute is an @attribute(via:) path, drawn solid.
	bindingsGraphAttribute bindingsGraphKind = iota
	// bindingsGraphDomain is an @domain(via:) path, drawn dotted.
	bindingsGraphDomain
	// bindingsGraphSubject is a dotted @subjectSet / @subjectValue value,
	// drawn dashed from the requester through the anchor.
	bindingsGraphSubject
)

// style renders the kind's edge attribute prefix.
func (k bindingsGraphKind) style() string {
	switch k {
	case bindingsGraphDomain:
		return "style=dotted, "
	case bindingsGraphSubject:
		return "style=dashed, "
	default:
		return ""
	}
}

// bindingsGraphSubjectNode is the requester's node, where every subject path starts.
const bindingsGraphSubjectNode = "subject"

// bindingsGraphEdge is one drawn hop. Label carries the binding name and the
// column the hop leaves through, and the column read where the path
// terminates, so two paths sharing a hop under one name are one edge.
type bindingsGraphEdge struct {
	Source string
	Target string
	Kind   bindingsGraphKind
	Label  string
}

// bindingsGraph is one package's assembled graph: the resources declaring a
// path, the tables a path only lands on, whether the requester is drawn, and
// the deduplicated edges, every list sorted so the rendered file is
// byte-stable across runs.
type bindingsGraph struct {
	Declaring []string
	Landed    []string
	Subject   bool
	Edges     []bindingsGraphEdge
}

// assembleBindingsGraph builds the graph from the parsed resources' compiled
// bindings: every attribute, domain, and subject binding with a non-empty
// path, hop by hop. Bare column bindings are legible where they are declared
// and are not drawn; a bare subject binding's requester correlation is not a
// hop. Landed tables resolve to the struct backing them where one is parsed,
// the table name otherwise, as the workflow graph does.
func (c *client) assembleBindingsGraph() *bindingsGraph {
	byTable := make(map[string]*resourceInfo, len(c.resources))
	for _, res := range c.resources {
		byTable[c.pluralize(res.Name())] = res
	}
	structName := func(table string) string {
		if res, ok := byTable[table]; ok {
			return res.Name()
		}

		return table
	}

	graph := &bindingsGraph{}
	declaring := make(map[string]bool)
	landed := make(map[string]bool)
	edges := make(map[bindingsGraphEdge]bool)

	// walk draws one path leaving source through column: each hop is an edge
	// into the hop's table labeled with the column left through, and the last
	// carries the column the path reads.
	walk := func(source, column string, path []bindingHop, kind bindingsGraphKind, name string) {
		declaring[source] = true
		for i, hop := range path {
			target := structName(hop.Table)
			label := name + ": " + column
			if i == len(path)-1 {
				label += " ⇒ " + hop.Column
			}
			edges[bindingsGraphEdge{Source: source, Target: target, Kind: kind, Label: label}] = true
			landed[target] = true
			source, column = target, hop.Column
		}
	}

	for _, res := range c.resources {
		for _, attr := range res.Attributes {
			if len(attr.Path) > 0 {
				walk(res.Name(), fieldColumn(attr.Anchor), attr.Path, bindingsGraphAttribute, attr.Name)
			}
		}
		if domain := res.DomainBinding; domain != nil && len(domain.Path) > 0 {
			walk(res.Name(), fieldColumn(domain.Anchor), domain.Path, bindingsGraphDomain, domainKeyword)
		}
		for _, subject := range slices.Concat(res.SubjectSets, res.SubjectValues) {
			if len(subject.Path) == 0 {
				continue
			}
			graph.Subject = true
			edges[bindingsGraphEdge{
				Source: bindingsGraphSubjectNode,
				Target: res.Name(),
				Kind:   bindingsGraphSubject,
				Label:  subject.Name + ": " + fieldColumn(subject.Anchor),
			}] = true
			walk(res.Name(), fieldColumn(subject.ValueField), subject.Path, bindingsGraphSubject, subject.Name)
		}
	}

	for name := range declaring {
		graph.Declaring = append(graph.Declaring, name)
	}
	slices.Sort(graph.Declaring)
	for name := range landed {
		if !declaring[name] {
			graph.Landed = append(graph.Landed, name)
		}
	}
	slices.Sort(graph.Landed)
	for edge := range edges {
		graph.Edges = append(graph.Edges, edge)
	}
	slices.SortFunc(graph.Edges, func(a, b bindingsGraphEdge) int {
		return cmp.Or(
			cmp.Compare(a.Kind, b.Kind),
			strings.Compare(a.Source, b.Source),
			strings.Compare(a.Target, b.Target),
			strings.Compare(a.Label, b.Label),
		)
	})

	return graph
}

// renderBindingsDOT draws the graph: solid boxes for the declaring resources,
// dashed boxes for the tables landed on, the requester as an ellipse, one
// labeled edge per hop in the kind's style, and a legend. Layout stays
// Graphviz's.
func renderBindingsDOT(graph *bindingsGraph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by resourcegeneration. DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Bindings: every join-path binding this package declares. Solid boxes are the\n")
	fmt.Fprintf(&b, "// resources declaring a path; dashed boxes are the tables a path only lands on;\n")
	fmt.Fprintf(&b, "// the subject ellipse is the requester. One edge per hop, drawn once however\n")
	fmt.Fprintf(&b, "// many paths share it: solid for @attribute, dotted for @domain, dashed for a\n")
	fmt.Fprintf(&b, "// subject binding, labeled `name: column` on a continuing hop and\n")
	fmt.Fprintf(&b, "// `name: column ⇒ column` where the path terminates. Facts only: no grant or\n")
	fmt.Fprintf(&b, "// condition is drawn.\n")
	fmt.Fprintf(&b, "digraph Bindings {\n")
	fmt.Fprintf(&b, "\trankdir=LR;\n")
	fmt.Fprintf(&b, "\tnode [shape=box];\n")
	for _, name := range graph.Declaring {
		fmt.Fprintf(&b, "\t%q;\n", name)
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
	fmt.Fprintf(&b, "\t\t\"declares a path\" [shape=box];\n")
	fmt.Fprintf(&b, "\t\t\"landed on\" [shape=box, style=dashed];\n")
	fmt.Fprintf(&b, "\t\t\"requester\" [shape=ellipse];\n")
	legend := []struct {
		id    string
		kind  bindingsGraphKind
		label string
	}{
		{id: attributeKeyword, kind: bindingsGraphAttribute, label: "@" + attributeKeyword},
		{id: domainKeyword, kind: bindingsGraphDomain, label: "@" + domainKeyword},
		{id: bindingsGraphSubjectNode, kind: bindingsGraphSubject, label: "@" + subjectSetKeyword + " / @" + subjectValueKeyword},
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
// package as zz_gen_bindings.dot, beside the workflow graphs. A package with
// no join-path binding writes no file; the generated-file sweep has already
// removed a stale one.
func (r *resourceGenerator) generateBindingsGraph() error {
	graph := r.assembleBindingsGraph()
	if len(graph.Edges) == 0 {
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
