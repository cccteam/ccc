package generation

import (
	"fmt"
	"log"
	"strings"
	"unicode"

	"github.com/cccteam/ccc/resource/generation/graph"
)

func Traversal(r *ResourceGenerator) {
	schemaGraph := graph.NewSQLGraph()
	for table, tableMeta := range r.client.tableLookup {
		for _, column := range tableMeta.Columns {
			if !column.IsForeignKey {
				continue
			}

			start := graph.NewTableColumn(table, column.ColumnName)
			end := graph.NewTableColumn(column.ReferencedTable, column.ReferencedColumn)

			if err := schemaGraph.AddRelation(start, end); err != nil {
				log.Fatal(err)

				return
			}
		}
	}

	start := graph.NewTableColumn("ClaimLoanSupplementalPayments", "ClaimId")
	end := graph.NewTableColumn("Claims", "StatusId")

	jc, err := JoinClause(schemaGraph.FindPath(start, end)...)
	if err != nil {
		fmt.Println(err)

		return
	}
	fmt.Printf("\n\n%v\n\n", jc)
}

func JoinClause(tableColumns ...graph.TableColumn) (string, error) {
	if len(tableColumns) == 0 {
		return "", nil
	}
	for _, c := range tableColumns {
		fmt.Printf(" %s ", c.String())
	}

	clause := strings.Builder{}
	clause.WriteString(fmt.Sprintf("FROM %s %s", tableColumns[0].Table(), Acronym(tableColumns[0].Table())))

	for i := range len(tableColumns) - 1 {
		if i%2 != 0 {
			continue
		}

		prev := tableColumns[i]
		curr := tableColumns[i+1]

		clause.WriteString("\n")
		clause.WriteString(joinClause(prev, curr))
	}

	return clause.String(), nil
}

func joinClause(from, to graph.TableColumn) string {
	fromAcr := Acronym(from.Table())
	toAcr := Acronym(to.Table())

	return fmt.Sprintf("JOIN %s %s ON %s.%s = %s.%s", to.Table(), toAcr, toAcr, to.Column(), fromAcr, from.Column())
}

func Acronym(s string) string {
	acr := strings.Builder{}
	for _, r := range s {
		if unicode.IsUpper(r) {
			acr.WriteRune(unicode.ToLower(r))
		}
	}

	return acr.String()
}
