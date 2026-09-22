package rpc

import (
	"context"
	"crypto/sha256"
	"io"
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
	// the keys: one MissionDocuments row per file, inside the transaction, whose commit
	// is what makes the objects the rows'; if anything before commit fails, the frame
	// deletes them. The Dispatcher's Execute grant carries the mission's state
	// (`state NOT IN ('completed', 'failed', 'stood_down')`): documents go on live
	// missions. Each row records its origin as a Provenance, one JSON column typed by a
	// plain struct, and the SHA-256 of its bytes as Digest, a BYTES column the body
	// computes from the object the frame streamed (a dry run streams nothing and records
	// no digest). Reading a document back is the generated file route on
	// MissionDocuments.StoreKey's @file.
	//
	// Demonstrates: @upload, rpc.upload-store, execute-condition, typescript.derived-object, typescript.byte-slice.
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
func (m *AttachMissionDocument) Execute(ctx context.Context, txn resource.ReadWriteTransaction, files resource.Files, client *Client) (*Attached, error) {
	uploadedBy := string(resource.CallerFrom(ctx).Permissions.User())
	now := time.Now().UTC()

	attached := &Attached{DocumentIDs: make([]ccc.UUID, 0, len(files))}
	for _, file := range files {
		patch, err := resources.NewMissionDocumentCreatePatch()
		if err != nil {
			return nil, errors.Wrap(err, "resources.NewMissionDocumentCreatePatch()")
		}
		// The digest is read off the stored object; a dry run minted no key and
		// streamed nothing, and writes no row, so there is nothing to digest.
		if file.Key != "" {
			digest, err := client.digest(ctx, file.Key)
			if err != nil {
				return nil, err
			}
			patch.SetDigest(digest)
		}
		patch.SetMissionID(m.MissionID).
			SetTitle(m.Title).
			SetFileName(file.Name).
			SetContentType(file.ContentType).
			SetSize(file.Size).
			SetStoreKey(file.Key).
			SetUploadedBy(uploadedBy).
			SetUploadedAt(now).
			// The origin rides as one JSON column: the struct is stored by its
			// generated Spanner methods and read back as the derived interface.
			SetProvenance(&resources.Provenance{System: "console-upload", Reference: file.Name, ReceivedAt: now})
		if err := patch.Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
			return nil, errors.Wrap(err, "resources.MissionDocumentCreatePatch.Buffer()")
		}
		attached.DocumentIDs = append(attached.DocumentIDs, patch.ID())
	}

	return attached, nil
}

// digest is the SHA-256 of the object the frame streamed under key, read back from the
// store.
func (c *Client) digest(ctx context.Context, key string) ([]byte, error) {
	content, err := c.documents.Open(ctx, key)
	if err != nil {
		return nil, errors.Wrap(err, "store.DirStore.Open()")
	}
	defer content.Body.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, content.Body); err != nil {
		return nil, errors.Wrap(err, "io.Copy()")
	}

	return sum.Sum(nil), nil
}
