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
	Get(v T) Vertex[T]    // Returns a reference to the Vertex for v if it exists, otherwise nil.
	Insert(v T) Vertex[T] // Inserts a value into the graph and returns a reference to its Vertex.
	Remove(value T)
	AddPath(src, dst Vertex[T])
	Vertices() iter.Seq[Vertex[T]] // Returns an unordered iterator over the graph's vertices.
	OrderedList(compare func(a T, b T) int) []T
	CycleCheck() error
	cycle(start T, key T, stack *[]T, blocked map[T]struct{}) bool
	exists(v T) bool
}

type Vertex[T comparable] interface {
	Value() T
	Indegree() int
	Outdegree() int
	addIncomingEdge(value T)
	addOutgoingEdge(value T)
	removeIncomingEdge(value T)
	removeOutgoingEdge(value T)
	isVertex()
}

type vertex[T comparable] struct {
	value T
	// Outgoing and incoming are sets of keys used to access the nodes in the parent graph's map.
	// This keeps the pointers in the parent graph so you can instantiate a graph at compile time.
	mu       sync.RWMutex
	outgoing map[T]struct{}
	incoming map[T]struct{}
}

func (v *vertex[T]) Value() T {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return v.value
}

func (v *vertex[T]) Indegree() int {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return len(v.incoming)
}

func (v *vertex[T]) Outdegree() int {
	v.mu.RLock()
	defer v.mu.RUnlock()

	return len(v.outgoing)
}

func (v *vertex[T]) addIncomingEdge(value T) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.incoming[value] = struct{}{}
}

func (v *vertex[T]) addOutgoingEdge(value T) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.outgoing[value] = struct{}{}
}

func (v *vertex[T]) removeIncomingEdge(value T) {
	v.mu.Lock()
	defer v.mu.Unlock()

	delete(v.incoming, value)
}

func (v *vertex[T]) removeOutgoingEdge(value T) {
	v.mu.Lock()
	defer v.mu.Unlock()

	delete(v.outgoing, value)
}

func (*vertex[T]) isVertex() {}

type graph[T comparable] struct {
	mu    sync.RWMutex
	graph map[T]*vertex[T]
}

func New[T comparable](capacity uint) Graph[T] {
	g := &graph[T]{
		graph: make(map[T]*vertex[T], capacity),
	}

	return g
}

func (g *graph[T]) Get(v T) Vertex[T] {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.exists(v) {
		return g.graph[v]
	}

	return nil
}

func (g *graph[T]) Insert(v T) Vertex[T] {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.exists(v) {
		g.graph[v] = &vertex[T]{
			outgoing: make(map[T]struct{}),
			incoming: make(map[T]struct{}),
		}
	}

	return g.graph[v]
}

func (g *graph[T]) Remove(value T) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.graph, value)

	for _, vertex := range g.graph {
		vertex.removeOutgoingEdge(value)
	}
}

func (g *graph[T]) AddPath(src, dst Vertex[T]) {
	src.addOutgoingEdge(dst.Value())
	dst.addIncomingEdge(src.Value())
}

func (g *graph[T]) Vertices() iter.Seq[Vertex[T]] {
	iter := func(yield func(Vertex[T]) bool) {
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

	var root *dependencyTree2[T]

	for key, vertex := range g.graph {
		root = addNode2(key, vertex.Outdegree(), root, compare)
	}

	return orderedTree2(root)
}

// Call site must have a lock on graph.mutex
func (g *graph[T]) exists(v T) bool {
	_, ok := g.graph[v]

	return ok
}

type dependencyTree2[T comparable] struct {
	key         T
	value       int
	left, right *dependencyTree2[T]
}

func newNode2[T comparable](key T, value int) *dependencyTree2[T] {
	return &dependencyTree2[T]{key: key, value: value}
}

func addNode2[T comparable](key T, value int, root *dependencyTree2[T], compare func(a, b T) int) *dependencyTree2[T] {
	switch {
	case root == nil:
		return newNode2(key, value)
	case value < root.value:
		root.left = addNode2(key, value, root.left, compare)
	case value > root.value:
		root.right = addNode2(key, value, root.right, compare)

	// if the values are equal, store the node sorted using compare func
	default:
		switch compare(key, root.key) {
		case -1:
			root.left = addNode2(key, value, root.left, compare)
		case 0:
			root.left = addNode2(key, value, root.left, compare)
		case 1:
			root.right = addNode2(key, value, root.right, compare)
		default:
			panic(fmt.Sprintf("invalid compare func want=(-1, 0, or 1) got=%d", compare(key, root.key)))
		}
	}

	return root
}

func orderedTree2[T comparable](root *dependencyTree2[T]) []T {
	if root == nil {
		return []T{}
	}

	left := orderedTree2(root.left)
	right := orderedTree2(root.right)
	left = append(left, root.key)

	return append(left, right...)
}
