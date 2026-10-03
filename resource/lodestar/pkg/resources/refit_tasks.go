package resources

import "github.com/cccteam/ccc"

type (
	// RefitTask is a checklist item: interleaved in Refits with a compound primary key,
	// so creates supply the full key (no server-generated UUID). The @stateRoot marker on
	// the anchoring foreign key makes it a member of the Refit workflow one hop from the
	// root, and its tenancy is THREE hops away: Refit, Ship, Hangar, SectorId.
	//
	// TaskNumber trails the parent key in the compound key with nothing bound before it
	// (the tenancy is a join path, so no column on the row is bound by equality): it is
	// not indexed, so the generated list struct carries no index tag on it, its metadata
	// says nothing about filtering, and a filter naming it alone is refused.
	//
	// A create that supplies a task number the refit already holds reaches the commit,
	// where Spanner refuses the duplicate key; the library answers 409 naming RefitTasks.
	//
	// PhotoKey's @file(photo) stores a photograph of the finished work under the task's
	// read route (.../refit-tasks/{id}/{taskNumber}/photo). Its key is a plain *string,
	// so the photo lives in the default store (APP_FILE_STORE), where the mission
	// documents' keys are typed resource.Key[Documents] and live in the Documents store:
	// the photographer's AttachRefitPhoto, an @upload with no store argument, streams
	// to the default store and sets this column. Nullable, it leaves Create ordinary,
	// and no seeded task carries one. It is also here for the audit pass: RefitTasks is
	// interleaved in Refits ON DELETE CASCADE, so a refit deleted by patch takes its
	// tasks with it through the database, never through the patch machinery, and the
	// release that deletes a stored object after the commit never runs for them; their
	// photos are the orphaned-file cleanup's (cmd/jobs cleanup-files). A normal
	// generation says nothing about that, since it would say so on every run;
	// `go run ./cmd/generate/resourcegenerator -audit` prints the one finding that
	// names this table and its parent, and cmd/generate/audit_test.go pins it.
	//
	// Demonstrates: interleaved-table, compound-key, client-supplied-key, @stateRoot, @domain.join-path, create-under-parent, index.trailing-key, commit.constraint-refusal, audit.cascade-release, filestore.named.
	//
	// @resource
	// @permissionScope(domain)
	// @order(TaskNumber asc)
	// @page(default: 25, max: 200)
	RefitTask struct {
		// The parent-key column is named Id because Spanner interleaving requires the
		// child's leading key column to carry the parent's key column name; the Go field
		// keeps the readable name.
		//
		// @stateRoot(Refit)
		// @domain(via: ShipID.HangarID.SectorID)
		RefitID      ccc.UUID `spanner:"Id"`
		TaskNumber   int64    `spanner:"TaskNumber"`
		Instructions string   `spanner:"Instructions"`
		Done         bool     `spanner:"Done"`
		Notes        *string  `spanner:"Notes"`
		// @file(photo)
		PhotoKey *string `spanner:"PhotoKey"`
	}
)
