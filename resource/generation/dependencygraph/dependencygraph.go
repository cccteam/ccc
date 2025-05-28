// Dependency Graph represents dependencies in our schema resources.
// Provides cycle checking and an ordered list representation of the schema.
// This is useful for generating schema files in a safe order.
package dependencygraph

import (
	"fmt"

	"github.com/go-playground/errors/v5"
)

type depGraph map[string]*graphNode

type Graph struct {
	depGraph
}

func New() *Graph {
	return &Graph{
		depGraph: make(depGraph),
	}
}

// Returns a slice of sorted resource names ordered by dependency
func (dg depGraph) OrderedList() []string {
	var root *dependencyTree

	for name, vertex := range dg {
		root = addNode(name, len(vertex.edges), root)
	}

	return orderedTree(root)
}

// Add a new dependency to the graph.
// src and dst can be new or pre-existing nodes in the graph.
func (dg depGraph) AddEdge(src, dst string) error {
	if cycle := dg.cycle(src, dst); cycle != "" {
		return errors.Newf("%s -> %q", cycle, src)
	}

	dg.addVertex(src)
	dg.addVertex(dst)

	dg[src].edges[dst] = dg[dst]

	return nil
}

func (dg depGraph) exists(name string) bool {
	_, ok := dg[name]

	return ok
}

func (dg depGraph) addVertex(name string) {
	if !dg.exists(name) {
		dg[name] = &graphNode{edges: make(map[string]*graphNode)}
	}
}

// TODO: consider moving cycle checking to a one-time check after graph is built
func (dg depGraph) cycle(src, dst string) string {
	if !dg.exists(src) || !dg.exists(dst) || src == dst {
		return ""
	}

	if _, ok := dg[dst].edges[src]; ok {
		return fmt.Sprintf("cyclical dependency: %q -> %q", src, dst)
	}

	for vert := range dg[dst].edges {
		if cycle := dg.cycle(src, vert); cycle != "" {
			return fmt.Sprintf("%s -> %q", cycle, dst)
		}
	}

	return ""
}

type graphNode struct {
	edges map[string]*graphNode // outgoing edges only
}

type dependencyTree struct {
	name        string
	outdegree   int
	left, right *dependencyTree
}

func newNode(name string, val int) *dependencyTree {
	return &dependencyTree{name: name, outdegree: val}
}

func addNode(name string, val int, root *dependencyTree) *dependencyTree {
	switch {
	case root == nil:
		return newNode(name, val)
	case val < root.outdegree:
		root.left = addNode(name, val, root.left)
	case val > root.outdegree:
		root.right = addNode(name, val, root.right)

	// if the values are equal, store the node sorted alphabetically
	case name < root.name:
		root.left = addNode(name, val, root.left)
	case name > root.name:
		root.right = addNode(name, val, root.right)
	}

	return root
}

func orderedTree(root *dependencyTree) []string {
	if root == nil {
		return []string{}
	}

	left := orderedTree(root.left)
	right := orderedTree(root.right)
	left = append(left, root.name)

	return append(left, right...)
}
