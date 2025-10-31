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

// ItemReader implements an interface where each Read() yeilds the next item
// from the Reader, with err returning any problems that occure during
// the Read() call. The semantics of how the end of stream is signaled
// is left up to the implementation.
//
// This can be used to convert the csv.Read() to an iter.Seq2 as an example.
type ItemReader[T any] interface {
	Read() (item T, err error)
}

// ItemIter returns a reusable iter.Seq2 iterator from anything that implements the ItemReader interface.
func ItemIter[T any](r ItemReader[T]) func(yield func(record T, err error) bool) {
	return func(yield func(record T, err error) bool) {
		for {
			record, err := r.Read()
			if !yield(record, err) {
				return
			}
		}
	}
}
