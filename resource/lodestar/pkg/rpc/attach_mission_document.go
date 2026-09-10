package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
)

type (
	// AttachMissionDocument attaches one or more files to a mission. It is the
	// method that UPLOADS: @upload(max: 5MB) makes the request multipart — the JSON
	// part first, then the files — and Execute takes resource.Files. The frame has
	// already bounded the body, decoded and checked the request exactly as a JSON
	// RPC, located the target mission within the sector, and streamed each file to
	// the sector's document store under a key it minted. The body's job is to claim
	// the keys: one MissionDocuments row per file, inside the transaction. After
	// commit the frame promotes the keys; if anything before commit fails, it
	// discards them. The Dispatcher's Execute grant carries the mission's state
	// (`state NOT IN ('completed', 'failed', 'stood_down')`): documents go on live
	// missions.
	//
	// Demonstrates: @upload, rpc.upload-store, execute-condition.
	//
	// @rpc
	// @permissionScope(domain)
	// @upload(max: 5MB)
	AttachMissionDocument struct {
		// @target(Mission)
		MissionID ccc.UUID
		// Title names the attachment; each file keeps its own file name.
		Title string
	}

	// Attached is the answer: the documents written, in the order the files came.
	Attached struct {
		DocumentIDs []ccc.UUID
	}
)

// Execute runs inside the handler's transaction with the streamed files.
func (m *AttachMissionDocument) Execute(ctx context.Context, txn resource.ReadWriteTransaction, files resource.Files, _ *Client) (*Attached, error) {
	uploadedBy := string(resource.CallerFrom(ctx).Permissions.User())
	now := time.Now().UTC()

	attached := &Attached{DocumentIDs: make([]ccc.UUID, 0, len(files))}
	for _, file := range files {
		patch, err := resources.NewMissionDocumentCreatePatch()
		if err != nil {
			return nil, errors.Wrap(err, "resources.NewMissionDocumentCreatePatch()")
		}
		patch.SetMissionID(m.MissionID).
			SetTitle(m.Title).
			SetFileName(file.Name).
			SetContentType(file.ContentType).
			SetSize(file.Size).
			SetStoreKey(file.Key).
			SetUploadedBy(uploadedBy).
			SetUploadedAt(now)
		if err := patch.Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
			return nil, errors.Wrap(err, "resources.MissionDocumentCreatePatch.Buffer()")
		}
		attached.DocumentIDs = append(attached.DocumentIDs, patch.ID())
	}

	return attached, nil
}
