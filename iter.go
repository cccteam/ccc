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
