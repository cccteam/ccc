package sqlbuilder

import (
	"io"

	"github.com/go-playground/errors/v5"
)

var _ Statement = &SelectStmt{}

type SelectStmt struct {
	columns Statement
	from    Statement
	where   Statement
	orderBy Statement

	error error
}

func Select(stmts ...Statement) *SelectStmt {
	if len(stmts) > 4 {
		return &SelectStmt{error: errors.New("called Select with more than four statements")}
	}

	var selectStmt SelectStmt
	for _, stmt := range stmts {
		switch t := stmt.SqlType(); t {
		case SqlColumns:
			if selectStmt.columns != nil {
				selectStmt.error = errors.Newf("called Select with multiple statements of type %s", t)

				return &selectStmt
			}

			selectStmt.columns = stmt
		case SqlFrom:
			if selectStmt.from != nil {
				selectStmt.error = errors.Newf("called Select with multiple statements of type %s", t)

				return &selectStmt
			}

			selectStmt.from = stmt
		case SqlWhere:
			if selectStmt.where != nil {
				selectStmt.error = errors.Newf("called Select with multiple statements of type %s", t)

				return &selectStmt
			}

			selectStmt.where = stmt
		case SqlOrderBy:
			if selectStmt.orderBy != nil {
				selectStmt.error = errors.Newf("called Select with multiple statements of type %s", t)

				return &selectStmt
			}

			selectStmt.orderBy = stmt
		}
	}

	return &selectStmt
}

func (s SelectStmt) SqlType() SqlType {
	return SqlSelect
}

func (s SelectStmt) WriteSql(w io.StringWriter) (n int, err error) {
	n, err = w.WriteString("SELECT\n\t")
	if err != nil {
		return n, err
	}

	if s.columns != nil {
		tmp, err := s.columns.WriteSql(w)
		if err != nil {
			return n, err
		}
		n += tmp
	}
	if s.from != nil {
		tmp, err := s.from.WriteSql(w)
		if err != nil {
			return n, err
		}
		n += tmp
	}
	if s.where != nil {
		tmp, err := s.where.WriteSql(w)
		if err != nil {
			return n, err
		}
		n += tmp
	}
	if s.orderBy != nil {
		tmp, err := s.orderBy.WriteSql(w)
		if err != nil {
			return n, err
		}
		n += tmp
	}

	return n, nil
}

func (s SelectStmt) Error() error {
	var err error

	if s.error != nil {
		return s.error
	}

	if s.columns == nil {
		return errors.Newf("%s statement missing required %s statement", SqlSelect, SqlColumns)
	}

	err = s.columns.Error()
	if err != nil {
		return err
	}

	if s.from != nil {
		err = s.from.Error()
	}
	if err != nil {
		return err
	}

	if s.where != nil {
		err = s.where.Error()
	}
	if err != nil {
		return err
	}

	if s.orderBy != nil {
		err = s.orderBy.Error()
	}
	if err != nil {
		return err
	}

	return nil
}
