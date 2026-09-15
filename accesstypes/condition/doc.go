// Package condition compiles and folds the ABAC condition language: a small
// expression language with a SQL-flavored surface in which grant conditions
// are written.
//
// The grammar (ABAC design plan §05, decided 2026-08-29):
//
//	condition  := or-expr
//	or-expr    := and-expr { OR and-expr }
//	and-expr   := unary { AND unary }
//	unary      := [ NOT ] primary
//	primary    := '(' or-expr ')' | comparison
//	comparison := attr ( = | != | < | <= | > | >= ) operand
//	            | attr [NOT] IN '(' literal { ',' literal } ')'
//	            | attr [NOT] IN subject.identifier      -- a @subjectSet name
//	            | attr IS [NOT] NULL
//	attr       := [ new. ] identifier | now | temporal
//	temporal   := ( timeOfDay | dayOfWeek ) '(' now ',' zone ')'
//	zone       := 'string' | local
//	operand    := literal | subject | now | subject.identifier | identifier
//	literal    := 'string' | number | true | false
//
// A bare identifier on a comparison's right side is an attribute reference —
// an old-vs-new comparison such as new.priority <= priority (§05, decided
// 2026-09-03) — and is admitted only against a new.-qualified left side; it
// never takes its own new. qualifier (two post-image sides would compare the
// proposed row with itself).
//
// With now on the left, the operand is a quoted string (an RFC 3339 instant),
// now itself, or subject.identifier; a number, a boolean, or bare subject is
// refused at parse, as now IN and now IS NULL are. The parser owns that rule,
// so deploy validation and the snapshot load refuse such a condition through
// the parse they already do, and the fold refuses a hand-built tree that
// bypassed the parser.
//
// The temporal functions (§05, decided 2026-09-03) read the environment's
// instant through a zone's wall clock: timeOfDay(now, zone) compares against
// 24-hour 'HH:MM' literals with the relational operators, and
// dayOfWeek(now, zone) against the day names 'mon' … 'sun' with =, != and
// [NOT] IN over a literal list. The zone is a quoted IANA name, or the bare
// word local — the Environment's zone attribute, resolved by the application.
// Temporal terms are environment facts: row-free, folded at check time in the
// engine, never rendered to SQL. Function names match case-sensitively; local
// is reserved only inside the zone argument.
//
// Keywords are case-insensitive; identifiers are case-sensitive, charset
// [A-Za-z_][A-Za-z0-9_]*. The reserved words subject, now, and new match
// case-sensitively, and a binding may not carry their names. Timestamps are
// quoted RFC 3339 strings typed by context; numbers are typed by context and
// kept verbatim; a string escapes a quote by writing it twice. Parse enforces
// generous fixed limits: 4 KB of source, nesting depth 32.
//
// # Evaluation
//
// A condition permits only when it is TRUE. Evaluation is three-valued, as in
// SQL: a comparison is TRUE, FALSE, or UNKNOWN; AND, OR, and NOT combine the
// three as SQL does (FALSE absorbs through AND, TRUE through OR, NOT leaves
// UNKNOWN as it is); and at the top only TRUE permits — a row survives a read,
// a cell shows, a write check passes, a capability holds, only when the
// condition is TRUE on that row. FALSE and UNKNOWN both refuse.
//
// A missing value is unknown in every form. A NULL column, a NULL foreign key
// on a join path, a join path that reaches no row, and a subject value whose
// requester has no anchor row are all "no value". Against no value a
// comparison is UNKNOWN, IN and NOT IN are UNKNOWN whether the list is
// literals or a subject set, IS NULL is TRUE, and IS NOT NULL is FALSE.
//
// A join-path attribute reads as the related row's column, or as no value. A
// subject value reads as the requester's anchor row's column, or as no value.
// A subject set is the non-null values the requester's anchor rows yield
// through the anchor's path, restricted to the request's partition when the
// request is partitioned and the anchor is domain-bound; a present value is
// IN an empty set FALSE and NOT IN it TRUE.
//
// A literal takes the attribute's type. A number literal compares exactly
// against an INT64 or a NUMERIC attribute and as a double against a FLOAT64
// attribute, whose stored value is already the nearest double; two stored
// numbers compare exactly unless either is a double. Timestamps compare as
// instants (RFC 3339 literals and now), dates as calendar days, strings
// bytewise in code-point order, booleans with FALSE before TRUE. The reference
// evaluator in conditiontest implements these rules over a row image; the
// resource package's semantic differential compares the SQL it renders against
// that evaluator on the Spanner emulator.
//
// The package owns the vocabulary only. Leaves are binding names, subject and
// subject attributes, now, and literals — no schema facts: the engine
// validates and folds (Fold, over Facts), the resource layer lowers binding
// names onto columns and join paths and renders SQL, and neither role is
// played here.
package condition
