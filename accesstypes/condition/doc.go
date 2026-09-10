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
// The package owns the vocabulary only. Leaves are binding names, subject and
// subject attributes, now, and literals — no schema facts: the engine
// validates and folds (Fold, over Facts), the resource layer lowers binding
// names onto columns and join paths and renders SQL, and neither role is
// played here.
package condition
