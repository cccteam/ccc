package ccc

import (
	"errors"
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

			for batch := range BatchIter2(tt.batch, inputIter) {
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

			for batch := range BatchIter2(tt.batch, inputIter) {
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

			for batch := range BatchIter2(tt.batch, inputIter) {
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

			for batch := range BatchIter2(10, inputIter) {
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
