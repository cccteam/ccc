package resource

import (
	"context"
	"fmt"
	"testing"
)

// MockSpannerBuffer is a mock implementation of SpannerBuffer for testing.
type MockSpannerBuffer struct {
	SpannerBufferFn func(ctx context.Context, txn *ReadWriteTransaction, eventSource ...string) error
	id              string
	Called          bool
}

func (m *MockSpannerBuffer) Buffer(ctx context.Context, txn *ReadWriteTransaction, eventSource ...string) error {
	m.Called = true
	if m.SpannerBufferFn != nil {
		return m.SpannerBufferFn(ctx, txn, eventSource...)
	}

	return nil
}

func TestNewCommitBuffer(t *testing.T) {
	t.Parallel()
	db := &Client{
		dbType: mockDBType,
	}
	eventSource := "test_source"
	autoCommitSize := 5

	cb := NewCommitBuffer(db, eventSource, autoCommitSize)

	if cb.client != db {
		t.Errorf("NewCommitBuffer() db = %v, want %v", cb.client, db)
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
		itemsToBuffer              []Buffer
		mockExecuteFuncShouldError bool
		spannerBufferShouldError   bool
		expectedErr                bool
		expectedBufferLenAfterOp   int
	}{
		{
			name:                     "Buffer items without auto-commit",
			autoCommitSize:           3,
			itemsToBuffer:            []Buffer{&MockSpannerBuffer{id: "1"}, &MockSpannerBuffer{id: "2"}},
			expectedErr:              false,
			expectedBufferLenAfterOp: 2,
		},
		{
			name:                     "Buffer items with auto-commit successful",
			autoCommitSize:           2,
			itemsToBuffer:            []Buffer{&MockSpannerBuffer{id: "1"}, &MockSpannerBuffer{id: "2"}},
			expectedErr:              false,
			expectedBufferLenAfterOp: 0,
		},
		{
			name:                     "Buffer items with auto-commit (more items than size, success)",
			autoCommitSize:           2,
			itemsToBuffer:            []Buffer{&MockSpannerBuffer{id: "1"}, &MockSpannerBuffer{id: "2"}, &MockSpannerBuffer{id: "3"}},
			expectedErr:              false,
			expectedBufferLenAfterOp: 0,
		},
		{
			name:                     "Buffer items with autoCommitSize 0 (no auto-commit)",
			autoCommitSize:           0,
			itemsToBuffer:            []Buffer{&MockSpannerBuffer{id: "1"}, &MockSpannerBuffer{id: "2"}},
			expectedErr:              false,
			expectedBufferLenAfterOp: 2,
		},
		{
			name:           "Auto-commit failure (Buffer error)",
			autoCommitSize: 2,
			itemsToBuffer: []Buffer{
				&MockSpannerBuffer{id: "sb1"},
				&MockSpannerBuffer{id: "sb2-fail", SpannerBufferFn: func(_ context.Context, _ *ReadWriteTransaction, _ ...string) error {
					return fmt.Errorf("spanner buffer error")
				}},
			},
			spannerBufferShouldError: true,
			expectedErr:              true,
			expectedBufferLenAfterOp: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &Client{
				dbType: mockDBType,
			}

			cb := NewCommitBuffer(client, "test_source", tt.autoCommitSize)

			if err := cb.Buffer(ctx, tt.itemsToBuffer...); (err != nil) != tt.expectedErr {
				t.Errorf("CommitBuffer.Buffer() error = %v, wantErr %v. Test: %s", err, tt.expectedErr, tt.name)
			}

			if len(cb.buffer) != tt.expectedBufferLenAfterOp {
				t.Errorf("CommitBuffer.Buffer() buffer length = %d, want %d. Test: %s", len(cb.buffer), tt.expectedBufferLenAfterOp, tt.name)
			}
		})
	}
}

func TestCommitBuffer_Commit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name                      string
		initialBufferItems        []Buffer
		spannerBufferShouldFail   bool
		failingSpannerBufferIndex int
		wantErr                   bool
		expectedBufferLenAfter    int
		expectedCalledItems       []bool
	}{
		{
			name:                   "Commit empty buffer",
			initialBufferItems:     []Buffer{},
			wantErr:                false,
			expectedBufferLenAfter: 0,
			expectedCalledItems:    []bool{},
		},
		{
			name: "Commit non-empty buffer successfully",
			initialBufferItems: []Buffer{
				&MockSpannerBuffer{id: "1"},
				&MockSpannerBuffer{id: "2"},
			},
			wantErr:                false,
			expectedBufferLenAfter: 0,
			expectedCalledItems:    []bool{true, true},
		},
		{
			name: "Commit non-empty buffer with SpannerBuffer error",
			initialBufferItems: []Buffer{
				&MockSpannerBuffer{id: "ok1"},
				&MockSpannerBuffer{id: "fail", SpannerBufferFn: func(_ context.Context, _ *ReadWriteTransaction, _ ...string) error {
					return fmt.Errorf("spanner buffer error from item")
				}},
				&MockSpannerBuffer{id: "ok2-notcalled"},
			},
			spannerBufferShouldFail:   true,
			failingSpannerBufferIndex: 1,
			wantErr:                   true,
			expectedBufferLenAfter:    3,
			expectedCalledItems:       []bool{true, true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.spannerBufferShouldFail && tt.failingSpannerBufferIndex < len(tt.initialBufferItems) {
				itemToFail, ok := tt.initialBufferItems[tt.failingSpannerBufferIndex].(*MockSpannerBuffer)
				if ok && itemToFail.SpannerBufferFn == nil {
					itemToFail.SpannerBufferFn = func(_ context.Context, _ *ReadWriteTransaction, _ ...string) error {
						return fmt.Errorf("forced spanner buffer error for %s", itemToFail.id)
					}
				}
			}

			mockDB := &Client{dbType: mockDBType}

			cb := NewCommitBuffer(mockDB, "test_source_commit", 0)
			if len(tt.initialBufferItems) > 0 {
				cb.buffer = append(cb.buffer, tt.initialBufferItems...)
			}

			if err := cb.Commit(ctx); (err != nil) != tt.wantErr {
				t.Errorf("CommitBuffer.Commit() error = %v, wantErr %v. Test: %s", err, tt.wantErr, tt.name)
			}

			if len(cb.buffer) != tt.expectedBufferLenAfter {
				t.Errorf("CommitBuffer.Commit() buffer length = %d, want %d. Test: %s", len(cb.buffer), tt.expectedBufferLenAfter, tt.name)
			}

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
		})
	}
}
