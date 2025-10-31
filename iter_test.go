package ccc

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIter2Batch(t *testing.T) {
	t.Parallel()

	type input struct {
		val int
		err error
	}

	testCases := []struct {
		name    string
		input   []input
		batch   int
		want    [][]int
		wantErr bool
		errText string
	}{
		{
			name:  "empty input",
			input: []input{},
			batch: 10,
			want:  nil,
		},
		{
			name: "batch size larger than input",
			input: []input{
				{val: 1, err: nil},
				{val: 2, err: nil},
				{val: 3, err: nil},
			},
			batch: 10,
			want:  [][]int{{1, 2, 3}},
		},
		{
			name: "batch size smaller than input",
			input: []input{
				{val: 1, err: nil},
				{val: 2, err: nil},
				{val: 3, err: nil},
				{val: 4, err: nil},
				{val: 5, err: nil},
			},
			batch: 2,
			want:  [][]int{{1, 2}, {3, 4}, {5}},
		},
		{
			name: "batch size equal to input",
			input: []input{
				{val: 1, err: nil},
				{val: 2, err: nil},
				{val: 3, err: nil},
			},
			batch: 3,
			want:  [][]int{{1, 2, 3}},
		},
		{
			name: "batch size of 1",
			input: []input{
				{val: 1, err: nil},
				{val: 2, err: nil},
				{val: 3, err: nil},
			},
			batch: 1,
			want:  [][]int{{1}, {2}, {3}},
		},
		{
			name:    "zero batch size",
			input:   nil,
			batch:   0,
			want:    nil,
			wantErr: true,
			errText: "invalid batch size 0",
		},
		{
			name:    "negative batch size",
			input:   nil,
			batch:   -1,
			want:    nil,
			wantErr: true,
			errText: "invalid batch size -1",
		},
		{
			name: "error in stream",
			input: []input{
				{val: 1, err: nil},
				{val: 0, err: errors.New("stream error")},
				{val: 3, err: nil},
			},
			batch:   5,
			want:    [][]int{{1, 3}},
			wantErr: true,
			errText: "stream error",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create an iter.Seq2 from the input slice
			inputIter := func(yield func(int, error) bool) {
				for _, v := range tt.input {
					if !yield(v.val, v.err) {
						return
					}
				}
			}

			var got [][]int
			var errs []error

			for batch := range BatchIter2(inputIter, tt.batch) {
				var currentBatch []int
				for item, err := range batch {
					if err != nil {
						errs = append(errs, err)
						continue
					}
					currentBatch = append(currentBatch, item)
				}
				if len(currentBatch) > 0 {
					got = append(got, currentBatch)
				}
			}

			if tt.wantErr {
				if len(errs) == 0 {
					t.Fatal("expected an error, but got none")
				}
				if !strings.Contains(errs[0].Error(), tt.errText) {
					t.Errorf("expected error text to contain '%s', got '%s'", tt.errText, errs[0].Error())
				}
			} else if len(errs) > 0 {
				t.Fatalf("unexpected error(s): %v", errs)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Iter2Batch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIter2Batch_struct(t *testing.T) {
	t.Parallel()

	type ts struct {
		value int
	}

	testCases := []struct {
		name    string
		input   []ts
		batch   int
		want    [][]ts
		wantErr bool
		errText string
	}{
		{
			name:  "empty input",
			input: []ts{},
			batch: 10,
			want:  nil,
		},
		{
			name:  "batch size smaller than input",
			input: []ts{{1}, {2}, {3}, {4}, {5}},
			batch: 2,
			want:  [][]ts{{{1}, {2}}, {{3}, {4}}, {{5}}},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inputIter := func(yield func(ts, error) bool) {
				for _, v := range tt.input {
					if !yield(v, nil) {
						return
					}
				}
			}

			var got [][]ts
			var errs []error

			for batch := range BatchIter2(inputIter, tt.batch) {
				var currentBatch []ts
				for item, err := range batch {
					if err != nil {
						errs = append(errs, err)
						continue
					}
					currentBatch = append(currentBatch, item)
				}
				if len(currentBatch) > 0 {
					got = append(got, currentBatch)
				}
			}

			if tt.wantErr {
				if len(errs) == 0 {
					t.Fatal("expected an error, but got none")
				}
				// check that the error contains the expected text
				if !strings.Contains(errs[0].Error(), tt.errText) {
					t.Errorf("expected error text to contain '%s', got '%s'", tt.errText, errs[0].Error())
				}
				// Also check that no data was returned
				if len(got) > 0 {
					t.Errorf("expected no batches when an error occurs, but got %d", len(got))
				}

				return
			}

			if len(errs) > 0 {
				t.Fatalf("unexpected error(s): %v", errs)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Iter2Batch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIter2Batch_slice(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		input   [][]string
		batch   int
		want    [][][]string
		wantErr bool
		errText string
	}{
		{
			name:  "empty input",
			input: [][]string{},
			batch: 10,
			want:  nil,
		},
		{
			name:  "batch size smaller than input",
			input: [][]string{{"1"}, {"2"}, {"3"}, {"4"}, {"5"}},
			batch: 2,
			want:  [][][]string{{{"1"}, {"2"}}, {{"3"}, {"4"}}, {{"5"}}},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inputIter := func(yield func([]string, error) bool) {
				for _, v := range tt.input {
					if !yield(v, nil) {
						return
					}
				}
			}

			var got [][][]string
			var errs []error

			for batch := range BatchIter2(inputIter, tt.batch) {
				var currentBatch [][]string
				for item, err := range batch {
					if err != nil {
						errs = append(errs, err)
						continue
					}
					currentBatch = append(currentBatch, item)
				}
				if len(currentBatch) > 0 {
					got = append(got, currentBatch)
				}
			}

			if tt.wantErr {
				if len(errs) == 0 {
					t.Fatal("expected an error, but got none")
				}
				// check that the error contains the expected text
				if !strings.Contains(errs[0].Error(), tt.errText) {
					t.Errorf("expected error text to contain '%s', got '%s'", tt.errText, errs[0].Error())
				}
				// Also check that no data was returned
				if len(got) > 0 {
					t.Errorf("expected no batches when an error occurs, but got %d", len(got))
				}

				return
			}

			if len(errs) > 0 {
				t.Fatalf("unexpected error(s): %v", errs)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Iter2Batch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIter2Batch_shutdown(t *testing.T) {
	t.Parallel()

	type input struct {
		val int
		err error
	}

	testCases := []struct {
		name        string
		input       []input
		outterBreak bool
		inner       bool
	}{
		{
			name: "shutdown in outer iterator",
			input: []input{
				{val: 1, err: nil},
				{val: 2, err: nil},
				{val: 3, err: nil},
			},
			outterBreak: true,
		},
		{
			name: "shutdown in inner iterator on first item",
			input: []input{
				{val: 1, err: errors.New("error")},
				{val: 2, err: nil},
				{val: 3, err: nil},
			},
		},
		{
			name: "shutdown in inner iterator on second item",
			input: []input{
				{val: 1, err: nil},
				{val: 2, err: errors.New("error")},
				{val: 3, err: nil},
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			done := make(chan struct{})

			// Create an iter.Seq2 from the input slice
			inputIter := func(yield func(int, error) bool) {
				defer close(done)

				for _, v := range tt.input {
					if !yield(v.val, v.err) {
						return
					}
				}
			}

			for batch := range BatchIter2(inputIter, 10) {
				if tt.outterBreak {
					break
				}

				for _, err := range batch {
					if err != nil {
						break
					}
				}
			}

			select {
			case <-done:
			case <-time.After(time.Second):
				t.Errorf("iterator did not shutdown")
			}
		})
	}
}

// itemReaderMock is a mock implementation of the ReadIterator interface for testing.
type itemReaderMock[T any] struct {
	items []struct {
		val T
		err error
	}
	idx int
}

func (r *itemReaderMock[T]) Read() (T, error) {
	if r.idx >= len(r.items) {
		var zero T
		return zero, io.EOF
	}
	item := r.items[r.idx]
	r.idx++
	return item.val, item.err
}

func TestReadIter(t *testing.T) {
	t.Parallel()

	type input struct {
		val int
		err error
	}

	testCases := []struct {
		name    string
		input   []input
		want    []int
		wantErr bool
		errText string
	}{
		{
			name:  "empty input",
			input: []input{},
			want:  nil,
		},
		{
			name: "normal iteration",
			input: []input{
				{val: 1, err: nil},
				{val: 2, err: nil},
				{val: 3, err: nil},
			},
			want: []int{1, 2, 3},
		},
		{
			name: "error in stream",
			input: []input{
				{val: 1, err: nil},
				{val: 0, err: errors.New("stream error")},
				{val: 3, err: nil},
			},
			want:    []int{1, 3},
			wantErr: true,
			errText: "stream error",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockReader := &itemReaderMock[int]{
				items: make([]struct {
					val int
					err error
				}, len(tt.input)),
			}
			for i, v := range tt.input {
				mockReader.items[i] = v
			}

			iter := ReadIter(mockReader)

			var got []int
			var errs []error

			for item, err := range iter {
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					errs = append(errs, err)
					continue // Do not process the item if there was an error
				}
				got = append(got, item)
			}

			if tt.wantErr {
				if len(errs) == 0 {
					t.Fatal("expected an error, but got none")
				}
				if !strings.Contains(errs[0].Error(), tt.errText) {
					t.Errorf("expected error text to contain '%s', got '%s'", tt.errText, errs[0].Error())
				}
			} else if len(errs) > 0 {
				t.Fatalf("unexpected error(s): %v", errs)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReadIter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadIter_shutdown(t *testing.T) {
	t.Parallel()

	readerDone := make(chan struct{})

	mockReader := &itemReaderMock[int]{
		items: []struct {
			val int
			err error
		}{{1, nil}, {2, nil}, {3, nil}},
	}

	// Wrap the mock reader to signal when the iteration is over
	inputIter := func(yield func(int, error) bool) {
		defer close(readerDone)
		// The actual iterator from the function under test
		iter := ReadIter(mockReader)
		for item, err := range iter {
			if !yield(item, err) {
				return
			}
			// Manually check for EOF as the consumer would
			if errors.Is(err, io.EOF) {
				return
			}
		}
	}

	// Consume only the first item and then break
	for range inputIter {
		break
	}

	select {
	case <-readerDone:
	case <-time.After(time.Second):
		t.Errorf("iterator did not shutdown when consumer stopped")
	}
}

func TestReadIter_continue(t *testing.T) {
	t.Parallel()

	mockReader := &itemReaderMock[int]{
		items: []struct {
			val int
			err error
		}{
			{1, nil},
			{2, nil},
			{3, nil},
			{4, nil},
			{5, nil},
		},
	}

	iter := ReadIter(mockReader)
	got := make([]int, 0, len(mockReader.items))

	// First loop, consume two items and break
	count := 0
	for item, err := range iter {
		if errors.Is(err, io.EOF) {
			break
		}
		got = append(got, item)
		count++
		if count >= 2 {
			break
		}
	}

	// Second loop, consume the rest
	for item, err := range iter {
		if errors.Is(err, io.EOF) {
			break
		}
		got = append(got, item)
	}

	want := []int{1, 2, 3, 4, 5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadIter() continuation failed: got %v, want %v", got, want)
	}
}

// nextIteratorMock is a mock implementation of the NextIterator interface for testing.
type nextIteratorMock[T any] struct {
	items    []T
	finalErr error
	idx      int
	val      T
}

func (r *nextIteratorMock[T]) Next() bool {
	if r.idx >= len(r.items) {
		return false
	}
	r.val = r.items[r.idx]
	r.idx++

	return true
}

func (r *nextIteratorMock[T]) Value() T {
	return r.val
}

func (r *nextIteratorMock[T]) Error() error {
	return r.finalErr
}

func TestNextIter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    []int
		finalErr error
		want     []int
		wantErr  bool
		errText  string
	}{
		{
			name:  "empty input",
			input: []int{},
			want:  nil,
		},
		{
			name:  "normal iteration",
			input: []int{1, 2, 3},
			want:  []int{1, 2, 3},
		},
		{
			name:     "iteration with final error",
			input:    []int{1, 2, 3},
			finalErr: errors.New("final error"),
			want:     []int{1, 2, 3, 0},
			wantErr:  true,
			errText:  "final error",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockIter := &nextIteratorMock[int]{
				items:    tt.input,
				finalErr: tt.finalErr,
			}

			var got []int
			var errs []error
			for item, err := range NextIter(mockIter) {
				if err != nil {
					errs = append(errs, err)
				}
				got = append(got, item)
			}

			if tt.wantErr {
				if len(errs) == 0 {
					t.Fatal("expected an error, but got none")
				}
				if !strings.Contains(errs[0].Error(), tt.errText) {
					t.Errorf("expected error text to contain '%s', got '%s'", tt.errText, errs[0].Error())
				}
			} else if len(errs) > 0 {
				t.Fatalf("unexpected error(s): %v", errs)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NextIter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextIter_shutdown(t *testing.T) {
	t.Parallel()

	shutdown := make(chan struct{})
	mockIter := &nextIteratorMock[int]{
		items: []int{1, 2, 3, 4, 5},
	}

	// Wrap the mock iterator to signal when the iteration is over
	inputIter := func(yield func(int, error) bool) {
		defer close(shutdown)
		iter := NextIter(mockIter)
		for item, err := range iter {
			if !yield(item, err) {
				return
			}
		}
	}

	// Consume only the first item and then break
	for range inputIter {
		break
	}

	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Errorf("iterator did not shutdown when consumer stopped")
	}
}

func TestNextIter_continue(t *testing.T) {
	t.Parallel()

	mockIter := &nextIteratorMock[int]{
		items: []int{1, 2, 3, 4, 5},
	}

	iter := NextIter(mockIter)
	got := make([]int, 0, len(mockIter.items))

	// First loop, consume two items and break
	count := 0
	for item := range iter {
		got = append(got, item)
		count++
		if count >= 2 {
			break
		}
	}

	// Second loop, consume the rest
	for item, err := range iter {
		if err != nil {
			// Also append the value that comes with the error, per implementation
			got = append(got, item)
			break
		}
		got = append(got, item)
	}

	want := []int{1, 2, 3, 4, 5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NextIter() continuation failed: got %v, want %v", got, want)
	}
}
