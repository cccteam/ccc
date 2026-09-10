package app

import (
	"fmt"
	"net/http"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// MissionDocumentContent serves a mission document's bytes. Reading a file back is
// the application's own route — the frame owns only intake and lifecycle — so the
// handler is hand-written: it checks Read on MissionDocuments and its storeKey in
// the URL sector's partition, fail-closed, reads the row and its mission, confirms
// the mission is the sector's, and streams the object from the store. A
// Conditional decision is refused, as on every hand-written surface: there is no
// generated statement here for the engine's conditions to render into, so only an
// unconditional grant opens the download — the console's crew have one; the client
// portal lists documents and does not download them.
//
// Demonstrates: rpc.upload-store, hand-written-route.
func (a *App) MissionDocumentContent() http.HandlerFunc {
	missionDocuments := resources.MissionDocument{}.Resource()
	storeKey := accesstypes.Resource(string(missionDocuments) + ".storeKey")

	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		domain := httpio.Param[accesstypes.Domain](r, router.Domain)
		id := httpio.Param[ccc.UUID](r, router.MissionDocumentID)

		env := accesstypes.NewEnvironment().WithNow(time.Now())
		decisions, err := a.UserPermissions(r).Check(ctx, env, accesstypes.DomainScope(domain), accesstypes.Read, missionDocuments, storeKey)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.UserPermissions.Check()"))
		}
		if !decisions[missionDocuments].IsGranted() || !decisions[storeKey].IsGranted() {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewForbiddenMessagef(
				"user %s does not have Read on %s in %s", a.UserPermissions(r).User(), missionDocuments, domain,
			))
		}

		txn := a.resourceClient.ReadOnlyTransaction()
		defer txn.Close()

		document, err := resources.NewMissionDocumentQuery().AddColumns(resources.NewMissionDocumentColumns().All()).SetID(id).Read(ctx, txn)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resources.MissionDocumentQuery.Read()"))
		}
		if document == nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewNotFoundMessagef("mission document %s does not exist", id))
		}
		mission, err := resources.NewMissionQuery().AddColumns(resources.NewMissionColumns().SectorID()).SetID(document.Data.MissionID).Read(ctx, txn)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resources.MissionQuery.Read()"))
		}
		if mission == nil || mission.Data.SectorID != string(domain) {
			// Another sector's document is indistinguishable from none.
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewNotFoundMessagef("mission document %s does not exist", id))
		}

		f, err := a.documents.Open(document.Data.StoreKey)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "store.DirStore.Open()"))
		}
		defer f.Close()

		w.Header().Set("Content-Type", document.Data.ContentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", document.Data.FileName))
		http.ServeContent(w, r, document.Data.FileName, document.Data.UploadedAt, f)

		return nil
	})
}
