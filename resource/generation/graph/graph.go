package graph

import (
	"errors"
	"slices"
)

type TableColumn struct {
	table  string
	column string
}

func NewTableColumn(table, column string) TableColumn {
	return TableColumn{
		table:  table,
		column: column,
	}
}

func (t TableColumn) Table() string {
	return t.table
}

func (t TableColumn) Column() string {
	return t.column
}

func (t TableColumn) String() string {
	return t.table + ":" + t.column
}

type SQLGraph struct {
	undirected map[TableColumn][]TableColumn

	sealed bool
}

func NewSQLGraph() *SQLGraph {
	return &SQLGraph{
		undirected: make(map[TableColumn][]TableColumn),
	}
}

func (sg *SQLGraph) AddRelation(start, end TableColumn) error {
	if sg.sealed {
		return errors.New("attempted to add to a sealed graph")
	}

	sg.undirected[start] = append(sg.undirected[start], end)
	sg.undirected[end] = append(sg.undirected[end], start)

	return nil
}

func (sg SQLGraph) Undirected() map[TableColumn][]TableColumn {
	return sg.undirected
}

func (sg *SQLGraph) FindPath(root, goal TableColumn) []TableColumn {
	if !sg.sealed {
		sg.Seal()
	}

	q := queue{}
	q.Enqueue(root)
	zero := TableColumn{}

	parent := make(map[TableColumn]TableColumn)
	parent[root] = zero

	for !q.Empty() {
		current := q.Dequeue()

		if current == goal {
			path := []TableColumn{}

			for current != zero {
				path = append(path, current)

				current = parent[current]
			}

			slices.Reverse(path)

			return path
		}

		for _, w := range sg.undirected[current] {
			_, explored := parent[w]
			if explored {
				continue
			}

			q.Enqueue(w)
			parent[w] = current
		}
	}

	return nil
}

func (sg *SQLGraph) Seal() {
	sg.sealed = true

	columnsInTable := make(map[string][]TableColumn)

	for tableColumn := range sg.undirected {
		columnsInTable[tableColumn.table] = append(columnsInTable[tableColumn.table], tableColumn)
	}

	for tableColumn := range sg.undirected {
		for _, otherColumn := range columnsInTable[tableColumn.table] {
			// column is self
			if otherColumn == tableColumn {
				continue
			}

			sg.undirected[tableColumn] = append(sg.undirected[tableColumn], otherColumn)
		}
	}
}

type queue struct {
	queue []TableColumn
}

func (q *queue) Empty() bool {
	return len(q.queue) == 0
}

func (q *queue) Enqueue(s TableColumn) {
	q.queue = append(q.queue, s)
}

func (q *queue) Dequeue() TableColumn {
	if len(q.queue) == 0 {
		return TableColumn{}
	}
	item := q.queue[0]

	q.queue = q.queue[1:]

	return item
}
