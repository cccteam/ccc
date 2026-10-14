package deploy

import (
	"context"
	"encoding/json"
	"os"

	"github.com/go-playground/errors/v5"
)

// An environment's stack has an id: a random_id the stack makes when it is first applied
// (initialize.tf), the suffix of the names Google keeps for a while after a delete (the
// Cloud Tasks queue, the Firebase API key, the migrate log bucket), so a stack destroyed
// and created again takes names of its own and never collides with the soft-deleted
// ones. The id is also the prefix of the stack's deployment records
// (<app>/<env>/<id>/releases/... and <app>/<env>/<id>/database/...): a new stack starts
// a history of its own, and the old stack's records stay where they were, read by
// nothing. The stack's apply writes the id to the records bucket
// (<app>/stacks/<env>/stack.json, {"id": "<hex>"}), and every reader of the
// environment's records starts there; no object means the stack has not been applied,
// and so has no records.
const (
	releasesSegment  = "releases"
	stackObjectName  = "stack.json"
	stackIDFieldName = "id"
)

// stackObject is the object under which the environment's stack says its id,
// <app>/stacks/<env>/stack.json, written by the stack's apply as the apply identity.
func stackObject(app, env string) string {
	return app + "/" + stacksSegment + "/" + env + "/" + stackObjectName
}

// stackID is the id of the environment's stack, read from the object its apply writes,
// or "" when there is no such object: the stack has not been applied.
func stackID(ctx context.Context, store Store, bucket, app, env string) (string, error) {
	object := stackObject(app, env)
	data, err := store.Read(ctx, bucket, object)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}

		return "", err
	}
	var pointer map[string]string
	if err := json.Unmarshal(data, &pointer); err != nil {
		return "", errors.Wrapf(err, "json.Unmarshal(): gs://%s/%s", bucket, object)
	}
	if pointer[stackIDFieldName] == "" {
		return "", errors.Newf("gs://%s/%s names no stack id (no %q)", bucket, object, stackIDFieldName)
	}

	return pointer[stackIDFieldName], nil
}

// appliedStackID is the id of the environment's stack as its apply wrote it to the records
// bucket, read through open (the deploy identity, which reads the bucket); a stack that
// says none has not been applied by a bedrock with stack ids, and is refused.
func appliedStackID(ctx context.Context, open StoreFunc, bucket, app, env string) (string, error) {
	store, err := open(ctx)
	if err != nil {
		return "", err
	}
	defer store.Close()
	id, err := stackID(ctx, store, bucket, app, env)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.Newf("%s's stack says no id (nothing at gs://%s/%s): the stack's apply writes it, so the stack has not been applied by a bedrock with stack ids; apply it once", env, bucket, stackObject(app, env))
	}

	return id, nil
}

// releasesPath is where the stack's release records lie: <app>/<env>/<id>/releases/,
// one directory per release, one object per build in it (releaseObject).
func releasesPath(app, env, id string) string {
	return app + "/" + env + "/" + id + "/" + releasesSegment + "/"
}

// releaseObject is a release record's object: <app>/<env>/<id>/releases/<release>/<build>.json.
func releaseObject(app, env, id, release, build string) string {
	return releasesPath(app, env, id) + release + "/" + build + ".json"
}

// releasesPrefix is the prefix of the environment's release records as the stack's id
// has it, or "" when the stack has not been applied, and so has none.
func releasesPrefix(ctx context.Context, store Store, bucket, app, env string) (string, error) {
	id, err := stackID(ctx, store, bucket, app, env)
	if err != nil || id == "" {
		return "", err
	}

	return releasesPath(app, env, id), nil
}

// newestLiveRelease is the environment's newest live record of a release: what the
// environment runs. Nil when the environment's stack has not been applied or no release
// is live in it.
func newestLiveRelease(ctx context.Context, store Store, bucket, app, env string) (*Record, error) {
	return newestLiveOf(ctx, store, bucket, app, env, "")
}

// newestLiveOf is the environment's newest live record of the release (of any release
// for ""), or nil.
func newestLiveOf(ctx context.Context, store Store, bucket, app, env, release string) (*Record, error) {
	prefix, err := releasesPrefix(ctx, store, bucket, app, env)
	if err != nil || prefix == "" {
		return nil, err
	}
	if release != "" {
		prefix += release + "/"
	}

	return newestRecordWhere(ctx, store, bucket, prefix, func(r *Record) bool { return r.Status == Live })
}
