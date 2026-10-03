package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// AttachRefitPhoto photographs a finished refit task: an @upload method on the default
// store, where the mission documents go to the Documents store. @upload(max: 5MB) with
// no store names the default, Execute takes resource.Files, whose keys are plain
// strings, and the body records the one file's key in RefitTask.PhotoKey, a *string
// column with @file(photo), so the photo is served under the task's read route
// (.../refit-tasks/{refitId}/{taskNumber}/photo) and released with the row. The frame
// located the refit within the sector (@target(Refit)) before the body ran and streamed
// the photo to the default store under a minted key; the body's job is to claim the key
// on the task's row, and a task number the refit does not hold is 404 from the update.
// A dry run streams nothing and commits nothing. The Photographer's Execute grant is
// unconditional in her sector. With AttachMissionDocument beside it, one application
// streams to both of its stores, each key column typed for its own, and the two
// directories in development, or the two buckets on Cloud Run, hold what each row names.
//
// Demonstrates: @upload, rpc.upload-store, filestore.named.
//
// @rpc
// @permissionScope(domain)
// @upload(max: 5MB)
type AttachRefitPhoto struct {
	// @target(Refit)
	RefitID    ccc.UUID
	TaskNumber int64
}

// Execute runs inside the handler's transaction with the streamed photo.
func (m *AttachRefitPhoto) Execute(ctx context.Context, txn resource.ReadWriteTransaction, files resource.Files, _ *Client) error {
	if len(files) != 1 {
		return httpio.NewBadRequestMessagef("a photo is one file; got %d", len(files))
	}
	key := files[0].Key
	patch := resources.NewRefitTaskUpdatePatch(m.RefitID, m.TaskNumber).SetPhotoKey(&key)
	if err := patch.Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.RefitTaskUpdatePatch.Buffer()")
	}

	return nil
}
