// firestore.go reads what the application's Firestore database carries beyond the
// database itself: the composite indexes and the time-to-live policies of
// firestore.indexes.json and the security rules of firestore.rules, in the firestore
// directory beside the schema migrations, where the skeleton puts the files the
// resource package's live/firestore directory ships.

package derive

import (
	"encoding/json"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// FirestoreDir is the Firestore directory's name beside the schema migrations directory
// (schema/firestore): the composite indexes, the time-to-live policies and the security
// rules of the application's Firestore database, in the two files the Firebase tooling
// also reads.
const FirestoreDir = "firestore"

// The two files, by the names the Firebase tooling gives them.
const (
	FirestoreIndexesFile = "firestore.indexes.json"
	FirestoreRulesFile   = "firestore.rules"
)

// The values the indexes file takes, as the Firestore Admin API names them.
const (
	queryScopeCollection = "COLLECTION"
	orderAscending       = "ASCENDING"
	orderDescending      = "DESCENDING"
	arrayContains        = "CONTAINS"
)

var (
	queryScopes = []string{queryScopeCollection, "COLLECTION_GROUP", "COLLECTION_RECURSIVE"}
	orders      = []string{orderAscending, orderDescending}
	// resourceNameRE matches what a resource name in the stack may not carry.
	resourceNameRE = regexp.MustCompile(`[^a-z0-9_]+`)
)

// Firestore is what the stack applies to the application's Firestore database from the
// files beside the schema migrations: the composite indexes and the field settings of
// the indexes file, and the security rules.
type Firestore struct {
	// Dir is the root-relative directory the files are read from.
	Dir string
	// IndexesFile and RulesFile are the two files, root-relative.
	IndexesFile string
	RulesFile   string
	// Indexes are the composite indexes, in the file's order.
	Indexes []FirestoreIndex
	// Fields are the fields the file settles on their own (its fieldOverrides): a
	// time-to-live policy, the single-field indexes, or both; in the file's order.
	Fields []FirestoreField
	// Rules is the security rules source as the file holds it.
	Rules string
}

// FirestoreIndex is one composite index.
type FirestoreIndex struct {
	// Collection is the collection group the index is on.
	Collection string
	// QueryScope is the scope queries run at: COLLECTION as a rule.
	QueryScope string
	// Fields are the indexed fields, in the file's order.
	Fields []FirestoreIndexField
}

// Name is the index's resource name in the stack: the collection and the field paths,
// joined by underscores, with anything else a resource name may not carry replaced.
func (i *FirestoreIndex) Name() string {
	parts := make([]string, 0, len(i.Fields)+1)
	parts = append(parts, i.Collection)
	for _, f := range i.Fields {
		parts = append(parts, f.Path)
	}

	return resourceName(parts...)
}

// FirestoreIndexField is one field of a composite index: ordered, or an array field.
type FirestoreIndexField struct {
	Path string
	// Order is ASCENDING or DESCENDING for an ordered field, and empty for an array
	// field, whose ArrayConfig is CONTAINS.
	Order       string
	ArrayConfig string
}

// FirestoreField is one field the indexes file settles on its own.
type FirestoreField struct {
	// Collection is the collection group, Field the field path.
	Collection string
	Field      string
	// TTL reports a time-to-live policy on the field: a document is deleted once the
	// timestamp the field holds has passed.
	TTL bool
	// Indexes are the field's single-field indexes, as the file lists them, and
	// HasIndexes whether the file lists them at all: a listed empty set turns the field's
	// indexing off, an absent one leaves the database's default indexing.
	Indexes    []FirestoreSingleFieldIndex
	HasIndexes bool
}

// Name is the field's resource name in the stack: the collection and the field path.
func (f *FirestoreField) Name() string {
	return resourceName(f.Collection, f.Field)
}

// FirestoreSingleFieldIndex is one single-field index of a field.
type FirestoreSingleFieldIndex struct {
	QueryScope string
	// Order is ASCENDING or DESCENDING for an ordered index, and empty for an array
	// index, whose ArrayConfig is CONTAINS.
	Order       string
	ArrayConfig string
}

// resourceName joins the parts with underscores and replaces what a resource name may
// not carry.
func resourceName(parts ...string) string {
	return resourceNameRE.ReplaceAllString(strings.ToLower(strings.Join(parts, "_")), "_")
}

// indexesFile is firestore.indexes.json as the Firebase tooling writes it.
type indexesFile struct {
	Indexes []struct {
		CollectionGroup string       `json:"collectionGroup"`
		QueryScope      string       `json:"queryScope"`
		Fields          []indexField `json:"fields"`
	} `json:"indexes"`
	FieldOverrides []struct {
		CollectionGroup string        `json:"collectionGroup"`
		FieldPath       string        `json:"fieldPath"`
		TTL             bool          `json:"ttl"`
		Indexes         *[]indexField `json:"indexes"`
	} `json:"fieldOverrides"`
}

// indexField is one field entry of the file, in a composite index or in a field's own
// indexes.
type indexField struct {
	FieldPath   string `json:"fieldPath"`
	Order       string `json:"order"`
	ArrayConfig string `json:"arrayConfig"`
	QueryScope  string `json:"queryScope"`
}

// firestore reads the Firestore files when the code declares a database, and refuses a
// web API key declared without one, or at another level than the database: the stack
// derives both from the one database and sets them on the processes that construct its
// level.
func (m *Model) firestore(a *app.App) error {
	database := m.byRole(RoleFirestoreDatabase)
	if key := m.byRole(RoleFirebaseAPIKey); key != nil {
		if database == nil {
			return errors.Newf("%s (%s) names the web API key of a Firestore database, and the config package declares no database (%s)", key.Name, key.Declaration(), varFirestoreDatabase)
		}
		if key.Level != database.Level {
			return errors.Newf("%s (%s) is declared at the %s level and %s (%s) at the %s level; the stack sets both on the processes that construct the database's level, so they belong together", key.Name, key.Declaration(), key.Level, database.Name, database.Declaration(), database.Level)
		}
	}
	project := m.byRole(RoleFirestoreProject)
	if project != nil && database == nil {
		return errors.Newf("%s (%s) names the project of a Firestore database, and the config package declares no database (%s)", project.Name, project.Declaration(), varFirestoreDatabase)
	}
	if database == nil {
		return nil
	}
	if project == nil {
		return errors.Newf("%s (%s) declares a Firestore database, and the config package declares no variable for its project (%s): the stack sets it to the environment project, where the database is, since the Spanner project is the shared instance's in an environment that shares one", database.Name, database.Declaration(), varFirestoreProject)
	}
	if project.Level != database.Level {
		return errors.Newf("%s (%s) is declared at the %s level and %s (%s) at the %s level; the stack sets both on the processes that construct the database's level, so they belong together", project.Name, project.Declaration(), project.Level, database.Name, database.Declaration(), database.Level)
	}
	fs := &Firestore{Dir: path.Join(path.Dir(m.Schema.MigrationsDir), FirestoreDir)}
	fs.IndexesFile = path.Join(fs.Dir, FirestoreIndexesFile)
	fs.RulesFile = path.Join(fs.Dir, FirestoreRulesFile)
	indexes, err := readFirestoreFile(a, fs.IndexesFile, database)
	if err != nil {
		return err
	}
	if err := fs.parseIndexes(indexes); err != nil {
		return err
	}
	rules, err := readFirestoreFile(a, fs.RulesFile, database)
	if err != nil {
		return err
	}
	fs.Rules = string(rules)
	m.Firestore = fs

	return nil
}

// readFirestoreFile reads one of the two files, refusing a declared database without it
// by naming the path.
func readFirestoreFile(a *app.App, rel string, database *Variable) ([]byte, error) {
	data, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Newf("%s (%s) declares a Firestore database, and %s is missing: the stack applies the database's composite indexes, time-to-live policies and security rules from the files beside the schema migrations (the resource package's live/firestore directory ships them)", database.Name, database.Declaration(), rel)
		}

		return nil, errors.Wrapf(err, "os.ReadFile(): %s", rel)
	}

	return data, nil
}

// parseIndexes reads the indexes file: every composite index and every field override,
// each checked for the shape the Firestore Admin API takes.
func (fs *Firestore) parseIndexes(data []byte) error {
	var file indexesFile
	if err := json.Unmarshal(data, &file); err != nil {
		return errors.Wrapf(err, "json.Unmarshal(): %s", fs.IndexesFile)
	}
	for i, idx := range file.Indexes {
		where := fs.IndexesFile + ": indexes[" + strconv.Itoa(i) + "]"
		if idx.CollectionGroup == "" {
			return errors.Newf("%s names no collectionGroup", where)
		}
		if len(idx.Fields) == 0 {
			return errors.Newf("%s on %s lists no fields", where, idx.CollectionGroup)
		}
		scope, err := queryScope(idx.QueryScope, where)
		if err != nil {
			return err
		}
		index := FirestoreIndex{Collection: idx.CollectionGroup, QueryScope: scope}
		for j, f := range idx.Fields {
			if f.FieldPath == "" {
				return errors.Newf("%s.fields[%d] names no fieldPath", where, j)
			}
			order, array, err := fieldShape(f, where+".fields["+strconv.Itoa(j)+"]")
			if err != nil {
				return err
			}
			index.Fields = append(index.Fields, FirestoreIndexField{Path: f.FieldPath, Order: order, ArrayConfig: array})
		}
		fs.Indexes = append(fs.Indexes, index)
	}
	for i, o := range file.FieldOverrides {
		where := fs.IndexesFile + ": fieldOverrides[" + strconv.Itoa(i) + "]"
		if o.CollectionGroup == "" || o.FieldPath == "" {
			return errors.Newf("%s names no collectionGroup or no fieldPath", where)
		}
		field := FirestoreField{Collection: o.CollectionGroup, Field: o.FieldPath, TTL: o.TTL, HasIndexes: o.Indexes != nil}
		if o.Indexes != nil {
			for j, f := range *o.Indexes {
				at := where + ".indexes[" + strconv.Itoa(j) + "]"
				order, array, err := fieldShape(f, at)
				if err != nil {
					return err
				}
				scope, err := queryScope(f.QueryScope, at)
				if err != nil {
					return err
				}
				field.Indexes = append(field.Indexes, FirestoreSingleFieldIndex{QueryScope: scope, Order: order, ArrayConfig: array})
			}
		}
		fs.Fields = append(fs.Fields, field)
	}

	return nil
}

// queryScope reads a query scope, COLLECTION when the file says nothing.
func queryScope(scope, where string) (string, error) {
	if scope == "" {
		return queryScopeCollection, nil
	}
	if !slices.Contains(queryScopes, scope) {
		return "", errors.Newf("%s: queryScope %q is not one of %s", where, scope, strings.Join(queryScopes, ", "))
	}

	return scope, nil
}

// fieldShape reads a field entry's one shape: an order (ASCENDING or DESCENDING) or an
// arrayConfig (CONTAINS), never both and never neither.
func fieldShape(f indexField, where string) (order, array string, err error) {
	switch {
	case f.Order != "" && f.ArrayConfig != "":
		return "", "", errors.Newf("%s carries both an order and an arrayConfig; a field takes one", where)
	case f.Order != "":
		if !slices.Contains(orders, f.Order) {
			return "", "", errors.Newf("%s: order %q is not one of %s", where, f.Order, strings.Join(orders, ", "))
		}

		return f.Order, "", nil
	case f.ArrayConfig != "":
		if f.ArrayConfig != arrayContains {
			return "", "", errors.Newf("%s: arrayConfig %q is not %s", where, f.ArrayConfig, arrayContains)
		}

		return "", f.ArrayConfig, nil
	default:
		return "", "", errors.Newf("%s carries neither an order nor an arrayConfig", where)
	}
}
