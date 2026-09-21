package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// ReplaceMissionDocument points a mission document at a new file: an @upload method
// whose one file part the frame has streamed to the store under a minted key, and whose
// @target locates the document within the sector before the body runs. The body sets the
// row's key, file name, content type, size, digest, and provenance, and nothing else:
// the object the row held is released by the patch machinery, which records the old key
// on the transaction when the update sets the key column, and the resource client's
// executor deletes it from the store once the commit lands (resource.WithFileStore). A
// dry run streams nothing, sets no real key, and releases nothing, since nothing commits.
// The Registrar's Execute grant is unconditional: the register is hers in her sector.
//
// Demonstrates: @file.replaced, @upload.
//
// @rpc
// @permissionScope(domain)
// @upload(max: 5MB)
type ReplaceMissionDocument struct {
	// @target(MissionDocument)
	DocumentID ccc.UUID
}

// Execute runs inside the handler's transaction with the streamed file.
func (m *ReplaceMissionDocument) Execute(ctx context.Context, txn resource.ReadWriteTransaction, files resource.Files, client *Client) error {
	if len(files) != 1 {
		return httpio.NewBadRequestMessagef("a replacement is one file; got %d", len(files))
	}
	file := files[0]
	now := time.Now().UTC()

	patch := resources.NewMissionDocumentUpdatePatch(m.DocumentID)
	// The digest is read off the stored object; a dry run minted no key and streamed
	// nothing, so there is nothing to digest.
	if file.Key != "" {
		digest, err := client.digest(ctx, file.Key)
		if err != nil {
			return err
		}
		patch.SetDigest(digest)
	}
	patch.SetFileName(file.Name).
		SetContentType(file.ContentType).
		SetSize(file.Size).
		SetStoreKey(file.Key).
		SetProvenance(&resources.Provenance{System: "console-upload", Reference: file.Name, ReceivedAt: now})
	if err := patch.Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.MissionDocumentUpdatePatch.Buffer()")
	}

	return nil
}
