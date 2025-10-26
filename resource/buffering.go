package resource

import (
	"context"

	"github.com/go-playground/errors/v5"
)

// CommitBuffer provides a way to buffer Spanner mutations and commit them in batches.
// This can improve performance by reducing the number of round trips to the database.
type CommitBuffer struct {
	client         Executor
	eventSource    string
	autoCommitSize int
	buffer         []Buffer
}

// NewCommitBuffer returns a CommitBuffer that flushes the buffer when it reaches autoCommitSize, committing the content in a single transaction.
// A autoCommitSize of 0 means that the buffer will never be flushed automatically.
// The buffer can be flushed manually by calling Commit().
// Commit() must be called before discarding CommitBuffer to ensure all buffered mutations are committed.
func NewCommitBuffer(client Executor, eventSource string, autoCommitSize int) *CommitBuffer {
	return &CommitBuffer{
		client:         client,
		eventSource:    eventSource,
		autoCommitSize: autoCommitSize,
		buffer:         make([]Buffer, 0, autoCommitSize),
	}
}

// Buffer adds one or more SpannerBuffer items to the internal buffer.
// If the number of items in the buffer reaches the `autoCommitSize`, it will automatically trigger a commit.
func (cb *CommitBuffer) Buffer(ctx context.Context, ps ...Buffer) error {
	cb.buffer = append(cb.buffer, ps...)

	if cb.autoCommitSize > 0 && len(cb.buffer) >= cb.autoCommitSize {
		if err := cb.Commit(ctx); err != nil {
			return errors.Wrap(err, "CommitBufferrer.Commit()")
		}
	}

	return nil
}

// Commit manually triggers a commit of all items currently in the buffer.
// If the buffer is empty, this is a no-op. After a successful commit, the buffer is cleared.
func (cb *CommitBuffer) Commit(ctx context.Context) error {
	if len(cb.buffer) == 0 {
		return nil
	}

	if err := cb.client.ExecuteFunc(ctx, func(ctx context.Context, txn ReadWriteTransaction) error {
		for _, rs := range cb.buffer {
			if err := rs.Buffer(ctx, txn, cb.eventSource); err != nil {
				return errors.Wrap(err, "spanner.DB.Buffer()")
			}
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.DB.ExecuteFunc()")
	}

	cb.buffer = cb.buffer[:0]

	return nil
}
