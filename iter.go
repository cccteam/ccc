package ccc

import (
	"fmt"
	"iter"
)

// BatchIter2 takes an iter.Seq2 and returns an iter.Seq of iter.Seq2,
// where each inner iter.Seq2 yields a batch of T of the specified size.
//
// BatchIter2 returns a single-use iterator, but can take in a reusable iterator.
//
// The inner batch iterator must be fully consumed before the next outer iterator
// can be accessed. If the inner iterator is aborted, the outer iterator will
// also abort.
//
// If the provided size is not a positive integer, the returned iterator will
// yield a single error.
//
// Example:
//
//	for batch := range BatchIter2(myIter, 10) {
//		// Do some operation between batches such as start a new db transaction
//
//		for resource, err := range batch {
//			if err != nil {
//				log.Fatal(err)
//			}
//			fmt.Println(resource)
//		}
//	}
func BatchIter2[T any](iter2 iter.Seq2[T, error], size int) iter.Seq[iter.Seq2[T, error]] {
	var zero T

	return func(yield func(iter.Seq2[T, error]) bool) {
		if size <= 0 {
			yield(func(yield func(T, error) bool) {
				yield(zero, fmt.Errorf("invalid batch size %d, expected a positive integer", size))
			})

			return
		}

		next, stop := iter.Pull2(iter2)
		defer stop()

		var done bool
		for !done {
			done = true

			firstRecord, err, ok := next()
			if !ok {
				return
			}
			if !yield(func(yield func(T, error) bool) {
				if !yield(firstRecord, err) {
					return
				}

				count := 1
				for {
					if count >= size {
						done = false

						return
					}
					record, err, ok := next()
					if !ok {
						return
					}
					if !yield(record, err) {
						return
					}
					count++
				}
			}) {
				return
			}
		}
	}
}

// ReadIterator implements an interface where each Read() yields the next item
// from the Reader, with err returning any problems that occur during
// the Read() call. The semantics of how the end of stream is signaled
// is left up to the implementation.
//
// This can be used to convert the csv.Read() to an iter.Seq2 as an example.
type ReadIterator[T any] interface {
	Read() (item T, err error)
}

// ReadIter returns a reusable iter.Seq2 iterator from anything that implements the ReadIterator interface.
func ReadIter[T any](r ReadIterator[T]) iter.Seq2[T, error] {
	return func(yield func(record T, err error) bool) {
		for {
			record, err := r.Read()
			if !yield(record, err) {
				return
			}
		}
	}
}

// NextIterator implements an interface where each Next() yields the next item
// from the Reader, with Error() returning any problems that occur during
// the Next() call. This iterator will start with a Next() call. If Next()
// returns true, it will call Value(). This will continue until Next() returns
// false, at which point it will call Error() one time.
type NextIterator[T any] interface {
	Next() bool
	Value() T
	Error() error
}

// NextIter returns a reusable iter.Seq2 iterator from anything that implements the NextIterator interface.
func NextIter[T any](r NextIterator[T]) iter.Seq2[T, error] {
	var zero T

	return func(yield func(record T, err error) bool) {
		for r.Next() {
			if !yield(r.Value(), nil) {
				return
			}
		}
		if err := r.Error(); err != nil {
			yield(zero, err)
		}
	}
}
