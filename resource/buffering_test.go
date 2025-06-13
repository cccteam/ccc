package resource

import (
	"context"
	"fmt"
	"testing"

	"cloud.google.com/go/spanner"
)

// MockTxnBuffer is a mock implementation of TxnBuffer for testing.
type MockTxnBuffer struct {
	CommitFn      func() error
	RollbackFn    func() error
	BufferWriteFn func([]*spanner.Mutation) error
	QueryFn       func(ctx context.Context, statement spanner.Statement) *spanner.RowIterator
}

func (m *MockTxnBuffer) Commit() error {
	if m.CommitFn != nil {
		return m.CommitFn()
	}
	return nil
}

func (m *MockTxnBuffer) Rollback() error {
	if m.RollbackFn != nil {
		return m.RollbackFn()
	}
	return nil
}

// BufferWrite is a mock implementation of the BufferWrite method.
func (m *MockTxnBuffer) BufferWrite(mutations []*spanner.Mutation) error {
	if m.BufferWriteFn != nil {
		return m.BufferWriteFn(mutations)
	}
	return nil
}

// Query is a mock implementation of the Query method.
func (m *MockTxnBuffer) Query(ctx context.Context, statement spanner.Statement) *spanner.RowIterator {
	if m.QueryFn != nil {
		return m.QueryFn(ctx, statement)
	}
	// Return a default *spanner.RowIterator or nil.
	// For tests where Query is called but not explicitly mocked, returning nil might be appropriate.
	return nil
}

// MockTxnFuncRunner is a mock implementation of TxnFuncRunner for testing.
type MockTxnFuncRunner struct {
	ExecuteFuncFn func(ctx context.Context, fn func(context.Context, TxnBuffer) error) error
}

func (m *MockTxnFuncRunner) ExecuteFunc(ctx context.Context, fn func(context.Context, TxnBuffer) error) error {
	if m.ExecuteFuncFn != nil {
		return m.ExecuteFuncFn(ctx, fn)
	}
	return fn(ctx, &MockTxnBuffer{})
}

// MockSpannerBuffer is a mock implementation of SpannerBuffer for testing.
type MockSpannerBuffer struct {
	SpannerBufferFn func(ctx context.Context, txn TxnBuffer, eventSource ...string) error
	id              string
	Called          bool
}

func (m *MockSpannerBuffer) SpannerBuffer(ctx context.Context, txn TxnBuffer, eventSource ...string) error {
	m.Called = true
	if m.SpannerBufferFn != nil {
		return m.SpannerBufferFn(ctx, txn, eventSource...)
	}
	return nil
}

func TestNewCommitBuffer(t *testing.T) {
	t.Parallel()
	db := &MockTxnFuncRunner{}
	eventSource := "test_source"
	autoCommitSize := 5

	cb := NewCommitBuffer(db, eventSource, autoCommitSize)

	if cb.db != db {
		t.Errorf("NewCommitBuffer() db = %v, want %v", cb.db, db)
	}
	if cb.eventSource != eventSource {
		t.Errorf("NewCommitBuffer() eventSource = %s, want %s", cb.eventSource, eventSource)
	}
	if cb.autoCommitSize != autoCommitSize {
		t.Errorf("NewCommitBuffer() autoCommitSize = %d, want %d", cb.autoCommitSize, autoCommitSize)
	}
	if cb.buffer == nil {
		t.Errorf("NewCommitBuffer() buffer should not be nil")
	}
	if cap(cb.buffer) != autoCommitSize {
		t.Errorf("NewCommitBuffer() buffer capacity = %d, want %d", cap(cb.buffer), autoCommitSize)
	}
	if len(cb.buffer) != 0 {
		t.Errorf("NewCommitBuffer() buffer length = %d, want %d", len(cb.buffer), 0)
	}
}

func TestCommitBuffer_Buffer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name                       string
		autoCommitSize             int
		itemsToBuffer              []SpannerBuffer
		mockExecuteFuncShouldError bool
		spannerBufferShouldError   bool
		expectedErr                bool
		expectedBufferLenAfterOp   int
		expectCommitCall           bool
	}{
		{
			name:                     "Buffer items without auto-commit",
			autoCommitSize:           3,
			itemsToBuffer:            []SpannerBuffer{&MockSpannerBuffer{id: "1"}, &MockSpannerBuffer{id: "2"}},
			expectedErr:              false,
			expectedBufferLenAfterOp: 2,
			expectCommitCall:         false,
		},
		{
			name:                     "Buffer items with auto-commit successful",
			autoCommitSize:           2,
			itemsToBuffer:            []SpannerBuffer{&MockSpannerBuffer{id: "1"}, &MockSpannerBuffer{id: "2"}},
			expectedErr:              false,
			expectedBufferLenAfterOp: 0,
			expectCommitCall:         true,
		},
		{
			name:                     "Buffer items with auto-commit (more items than size, success)",
			autoCommitSize:           2,
			itemsToBuffer:            []SpannerBuffer{&MockSpannerBuffer{id: "1"}, &MockSpannerBuffer{id: "2"}, &MockSpannerBuffer{id: "3"}},
			expectedErr:              false,
			expectedBufferLenAfterOp: 0,
			expectCommitCall:         true,
		},
		{
			name:                     "Buffer items with autoCommitSize 0 (no auto-commit)",
			autoCommitSize:           0,
			itemsToBuffer:            []SpannerBuffer{&MockSpannerBuffer{id: "1"}, &MockSpannerBuffer{id: "2"}},
			expectedErr:              false,
			expectedBufferLenAfterOp: 2,
			expectCommitCall:         false,
		},
		{
			name:           "Auto-commit failure (SpannerBuffer error)",
			autoCommitSize: 2,
			itemsToBuffer: []SpannerBuffer{
				&MockSpannerBuffer{id: "sb1"},
				&MockSpannerBuffer{id: "sb2-fail", SpannerBufferFn: func(ctx context.Context, txn TxnBuffer, eventSource ...string) error {
					return fmt.Errorf("spanner buffer error")
				}},
			},
			spannerBufferShouldError: true,
			expectedErr:              true,
			expectedBufferLenAfterOp: 2,
			expectCommitCall:         true,
		},
		{
			name:                       "Auto-commit failure (ExecuteFunc error)",
			autoCommitSize:             2,
			itemsToBuffer:              []SpannerBuffer{&MockSpannerBuffer{id: "1"}, &MockSpannerBuffer{id: "2"}},
			mockExecuteFuncShouldError: true,
			expectedErr:                true,
			expectedBufferLenAfterOp:   2,
			expectCommitCall:           true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			commitCalledCount := 0

			mockDB := &MockTxnFuncRunner{
				ExecuteFuncFn: func(ctx context.Context, fn func(context.Context, TxnBuffer) error) error {
					commitCalledCount++
					if tt.mockExecuteFuncShouldError {
						return fmt.Errorf("mock db execute error")
					}
					return fn(ctx, &MockTxnBuffer{})
				},
			}

			cb := NewCommitBuffer(mockDB, "test_source", tt.autoCommitSize)
			err := cb.Buffer(ctx, tt.itemsToBuffer...)

			if (err != nil) != tt.expectedErr {
				t.Errorf("CommitBuffer.Buffer() error = %v, wantErr %v. Test: %s", err, tt.expectedErr, tt.name)
			}

			if len(cb.buffer) != tt.expectedBufferLenAfterOp {
				t.Errorf("CommitBuffer.Buffer() buffer length = %d, want %d. Test: %s", len(cb.buffer), tt.expectedBufferLenAfterOp, tt.name)
			}

			expectedCommitCalls := 0
			if tt.expectCommitCall {
				if tt.autoCommitSize > 0 && len(tt.itemsToBuffer) >= tt.autoCommitSize {
					expectedCommitCalls = 1
				}
			}

			if len(tt.itemsToBuffer) == 0 && tt.autoCommitSize > 0 {
				expectedCommitCalls = 0
			}

			if commitCalledCount != expectedCommitCalls {
				t.Errorf("CommitBuffer.Buffer() commitCalledCount = %d, want %d. Test: %s", commitCalledCount, expectedCommitCalls, tt.name)
			}
		})
	}
}

func TestCommitBuffer_Commit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name                      string
		initialBufferItems        []SpannerBuffer
		dbExecuteShouldFail       bool
		spannerBufferShouldFail   bool
		failingSpannerBufferIndex int
		wantErr                   bool
		expectedBufferLenAfter    int
		expectExecuteFuncCall     bool
		expectedCalledItems       []bool
	}{
		{
			name:                   "Commit empty buffer",
			initialBufferItems:     []SpannerBuffer{},
			wantErr:                false,
			expectedBufferLenAfter: 0,
			expectExecuteFuncCall:  false,
			expectedCalledItems:    []bool{},
		},
		{
			name: "Commit non-empty buffer successfully",
			initialBufferItems: []SpannerBuffer{
				&MockSpannerBuffer{id: "1"},
				&MockSpannerBuffer{id: "2"},
			},
			wantErr:                false,
			expectedBufferLenAfter: 0,
			expectExecuteFuncCall:  true,
			expectedCalledItems:    []bool{true, true},
		},
		{
			name: "Commit non-empty buffer with ExecuteFunc error",
			initialBufferItems: []SpannerBuffer{
				&MockSpannerBuffer{id: "1"},
			},
			dbExecuteShouldFail:    true,
			wantErr:                true,
			expectedBufferLenAfter: 1,
			expectExecuteFuncCall:  true,
			expectedCalledItems:    []bool{false},
		},
		{
			name: "Commit non-empty buffer with SpannerBuffer error",
			initialBufferItems: []SpannerBuffer{
				&MockSpannerBuffer{id: "ok1"},
				&MockSpannerBuffer{id: "fail", SpannerBufferFn: func(ctx context.Context, txn TxnBuffer, eventSource ...string) error {
					return fmt.Errorf("spanner buffer error from item")
				}},
				&MockSpannerBuffer{id: "ok2-notcalled"},
			},
			spannerBufferShouldFail:   true,
			failingSpannerBufferIndex: 1,
			wantErr:                   true,
			expectedBufferLenAfter:    3,
			expectExecuteFuncCall:     true,
			expectedCalledItems:       []bool{true, true, false},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			executeFuncCalled := false

			if tt.spannerBufferShouldFail && tt.failingSpannerBufferIndex < len(tt.initialBufferItems) {
				itemToFail, ok := tt.initialBufferItems[tt.failingSpannerBufferIndex].(*MockSpannerBuffer)
				if ok && itemToFail.SpannerBufferFn == nil {
					itemToFail.SpannerBufferFn = func(ctx context.Context, txn TxnBuffer, eventSource ...string) error {
						return fmt.Errorf("forced spanner buffer error for %s", itemToFail.id)
					}
				}
			}

			mockDB := &MockTxnFuncRunner{
				ExecuteFuncFn: func(ctx context.Context, fn func(context.Context, TxnBuffer) error) error {
					executeFuncCalled = true
					if tt.dbExecuteShouldFail {
						return fmt.Errorf("mock db execute error from ExecuteFuncFn")
					}
					return fn(ctx, &MockTxnBuffer{})
				},
			}

			cb := NewCommitBuffer(mockDB, "test_source_commit", 0)
			if len(tt.initialBufferItems) > 0 {
				cb.buffer = append(cb.buffer, tt.initialBufferItems...)
			}

			err := cb.Commit(ctx)

			if (err != nil) != tt.wantErr {
				t.Errorf("CommitBuffer.Commit() error = %v, wantErr %v. Test: %s", err, tt.wantErr, tt.name)
			}

			if len(cb.buffer) != tt.expectedBufferLenAfter {
				t.Errorf("CommitBuffer.Commit() buffer length = %d, want %d. Test: %s", len(cb.buffer), tt.expectedBufferLenAfter, tt.name)
			}

			if executeFuncCalled != tt.expectExecuteFuncCall {
				t.Errorf("CommitBuffer.Commit() executeFuncCalled = %v, want %v. Test: %s", executeFuncCalled, tt.expectExecuteFuncCall, tt.name)
			}

			if tt.expectExecuteFuncCall && !tt.dbExecuteShouldFail {
				if len(tt.initialBufferItems) != len(tt.expectedCalledItems) && len(tt.initialBufferItems) > 0 {
					t.Fatalf("Test setup error: length of initialBufferItems (%d) does not match length of expectedCalledItems (%d) for test: %s", len(tt.initialBufferItems), len(tt.expectedCalledItems), tt.name)
				}
				for i, item := range tt.initialBufferItems {
					msb, ok := item.(*MockSpannerBuffer)
					if !ok {
						t.Fatalf("Test setup error: item %d is not a MockSpannerBuffer in test: %s", i, tt.name)
					}
					if msb.Called != tt.expectedCalledItems[i] {
						t.Errorf("MockSpannerBuffer item %d ('%s') Called = %v, want %v. Test: %s", i, msb.id, msb.Called, tt.expectedCalledItems[i], tt.name)
					}
				}
			}
		})
	}
}
