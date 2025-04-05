package resource

import (
	"context"

	"github.com/go-playground/errors/v5"
)

type CommitBuffer struct {
	db             TxnFuncRunner
	eventSource    string
	autoCommitSize int
	buffer         []SpannerBuffer
}

// NewCommitBuffer returns a CommitBuffer that flushes the buffer when it reaches autoCommitSize, committing the content in a single transaction.
// A autoCommitSize of 0 means that the buffer will never be flushed automatically.
// The buffer can be flushed manually by calling Commit().
// Commit() must be called before discarding CommitBuffer to ensure all buffered mutations are committed.
func NewCommitBuffer(db TxnFuncRunner, eventSource string, autoCommitSize int) *CommitBuffer {
	return &CommitBuffer{
		db:             db,
		eventSource:    eventSource,
		autoCommitSize: autoCommitSize,
		buffer:         make([]SpannerBuffer, 0, autoCommitSize),
	}
}

func (cb *CommitBuffer) Buffer(ctx context.Context, ps ...SpannerBuffer) error {
	cb.buffer = append(cb.buffer, ps...)

	if cb.autoCommitSize > 0 && len(cb.buffer) >= cb.autoCommitSize {
		if err := cb.Commit(ctx); err != nil {
			return errors.Wrap(err, "CommitBufferrer.Commit()")
		}
	}

	return nil
}

func (cb *CommitBuffer) Commit(ctx context.Context) error {
	if len(cb.buffer) == 0 {
		return nil
	}

	if err := cb.db.ExecuteFunc(ctx, func(ctx context.Context, txn TxnBuffer) error {
		for _, rs := range cb.buffer {
			if err := rs.SpannerBuffer(ctx, txn, cb.eventSource); err != nil {
				return errors.Wrap(err, "spanner.DB.SpannerBuffer()")
			}
		}

		return nil
	}); err != nil {
		return errors.Wrap(err, "spanner.DB.ExecuteFunc()")
	}

	cb.buffer = cb.buffer[:0]

	return nil
}
