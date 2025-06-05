// Graph provides a threadsafe implementation of a directed graph. Useful for representing
// dependencies and detecting cycles.
package graph

import (
	"fmt"
	"iter"
	"sync"

	"github.com/go-playground/errors/v5"
)

type Graph[T comparable] interface {
	Get(n T) Node[T]    // Returns a reference to the Node for v if it exists, otherwise nil.
	Insert(n T) Node[T] // Inserts a value into the graph and returns a reference to its Node.
	Remove(n Node[T])
	Length() int
	AddPath(src, dst Node[T])
	Nodes() iter.Seq[Node[T]] // Returns an unordered iterator over the graph's nodes.
	OrderedList(compare func(a T, b T) int) []T
	CycleCheck() error
	cycle(start T, key T, stack *[]T, blocked map[T]struct{}) bool
	exists(v T) bool
}

type Node[T comparable] interface {
	Value() T
	Dependencies() []T
	NumDependents() int
	NumDependencies() int
	addDependent(value T)
	addDependency(value T)
	removeDependent(value T)
	removeDependency(value T)
	isNode()
}

type node[T comparable] struct {
	value T
	// Outgoing and incoming are sets of keys used to access the nodes in the parent graph's map.
	// This keeps the pointers in the parent graph so you can instantiate a graph at compile time.
	mu       sync.RWMutex
	outgoing map[T]struct{}
	incoming map[T]struct{}
}

func (v *node[T]) Value() T {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return v.value
}

func (v *node[T]) Dependencies() []T {
	v.mu.RLock()
	defer v.mu.RUnlock()

	set := make([]T, len(v.incoming))
	for n := range v.incoming {
		set = append(set, n)
	}

	return set
}

func (v *node[T]) NumDependents() int {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return len(v.incoming)
}

func (v *node[T]) NumDependencies() int {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return len(v.outgoing)
}

func (v *node[T]) addDependent(value T) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.incoming[value] = struct{}{}
}

func (v *node[T]) addDependency(value T) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.outgoing[value] = struct{}{}
}

func (v *node[T]) removeDependent(value T) {
	v.mu.Lock()
	defer v.mu.Unlock()

	delete(v.incoming, value)
}

func (v *node[T]) removeDependency(value T) {
	v.mu.Lock()
	defer v.mu.Unlock()

	delete(v.outgoing, value)
}

func (*node[T]) isNode() {}

type graph[T comparable] struct {
	mu    sync.RWMutex
	graph map[T]*node[T]
}

func New[T comparable](capacity uint) Graph[T] {
	g := &graph[T]{
		graph: make(map[T]*node[T], capacity),
	}

	return g
}

func (g *graph[T]) Length() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return len(g.graph)
}

func (g *graph[T]) Get(v T) Node[T] {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.exists(v) {
		return g.graph[v]
	}

	return nil
}

func (g *graph[T]) Insert(v T) Node[T] {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.exists(v) {
		g.graph[v] = &node[T]{
			value:    v,
			outgoing: make(map[T]struct{}),
			incoming: make(map[T]struct{}),
		}
	}

	return g.graph[v]
}

func (g *graph[T]) Remove(node Node[T]) {
	g.mu.Lock()
	defer g.mu.Unlock()

	key := node.Value()

	delete(g.graph, key)

	for _, node := range g.graph {
		node.removeDependency(key)
	}
}

func (g *graph[T]) AddPath(src, dst Node[T]) {
	src.addDependency(dst.Value())
	dst.addDependent(src.Value())
}

func (g *graph[T]) Nodes() iter.Seq[Node[T]] {
	iter := func(yield func(Node[T]) bool) {
		for _, vert := range g.graph {
			if !yield(vert) {
				return
			}
		}
	}

	return iter
}

func (g *graph[T]) CycleCheck() error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Using Johnson's algorithm for finding cycles in a directed graph: https://www.cs.tufts.edu/comp/150GA/homeworks/hw1/Johnson%2075.PDF
	// find a node with 0 outdegree and remove it
	// repeat
	// if a node with 0 outdegree cannot befound before the graph is empty there is a cycle
	for start := range g.graph {
		stack := []T{start}
		blocked := map[T]struct{}{start: {}}

		for key := range g.graph[start].outgoing {
			if g.cycle(start, key, &stack, blocked) {
				return errors.Newf("cycle detected: %v", stack)
			}
		}
	}

	return nil
}

// Call site must have a lock on Graph.mutex
func (g *graph[T]) cycle(start, key T, stack *[]T, blocked map[T]struct{}) bool {
	*stack = append(*stack, key)
	blocked[key] = struct{}{}

	for next := range g.graph[key].outgoing {
		if next == start {
			*stack = append(*stack, start)

			return true
		}

		if _, ok := blocked[next]; ok {
			continue
		}

		if g.cycle(start, next, stack, blocked) {
			return true
		}
	}

	return false
}

func (g *graph[T]) OrderedList(compare func(a, b T) int) []T {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var root *dependencyTree[T]

	for key, node := range g.graph {
		root = addNode(key, node.NumDependencies(), root, compare)
	}

	return orderedTree(root)
}

// Call site must have a lock on graph.mutex
func (g *graph[T]) exists(v T) bool {
	_, ok := g.graph[v]

	return ok
}

type dependencyTree[T comparable] struct {
	key         T
	value       int
	left, right *dependencyTree[T]
}

func newNode[T comparable](key T, value int) *dependencyTree[T] {
	return &dependencyTree[T]{key: key, value: value}
}

func addNode[T comparable](key T, value int, root *dependencyTree[T], compare func(a, b T) int) *dependencyTree[T] {
	switch {
	case root == nil:
		return newNode(key, value)
	case value < root.value:
		root.left = addNode(key, value, root.left, compare)
	case value > root.value:
		root.right = addNode(key, value, root.right, compare)

	// if the values are equal, store the node sorted using compare func
	default:
		switch compare(key, root.key) {
		case -1:
			root.left = addNode(key, value, root.left, compare)
		case 0:
			root.left = addNode(key, value, root.left, compare)
		case 1:
			root.right = addNode(key, value, root.right, compare)
		default:
			panic(fmt.Sprintf("invalid compare func want=(-1, 0, or 1) got=%d", compare(key, root.key)))
		}
	}

	return root
}

func orderedTree[T comparable](root *dependencyTree[T]) []T {
	if root == nil {
		return []T{}
	}

	left := orderedTree(root.left)
	right := orderedTree(root.right)
	left = append(left, root.key)

	return append(left, right...)
}
