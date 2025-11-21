package resource

import (
	"context"
	iter "iter"

	"github.com/cccteam/ccc/accesstypes"
)

type ExampleResource struct{}

func (ExampleResource) Resource() accesstypes.Resource {
	return "ExampleResources"
}

func (ExampleResource) DefaultConfig() Config {
	return Config{}
}

func ExampleQuerySet_BatchList() {
	ctx := context.Background()
	client := SpannerClient{}
	qSet := &QuerySet[ExampleResource]{}

	// Each batch iterator must be iterated on at least once before iterating on the next batch.
	// If a database iter.Seq2 errors, all following batches are invalid and iteration should halt.
	for batch := range qSet.BatchList(ctx, &client, 10) {
		if err := processExampleBatch(batch); err != nil {
			return
		}
	}

	// It's acceptable to break out of an iterator early and move onto the next batch iterator.
	for batch := range qSet.BatchList(ctx, &client, 10) {
		for res, err := range batch {
			if err != nil {
				return
			}

			if res.Resource() == "" {
				break
			}
		}
	}

	// BAD: Handing off batched iterators to go routines and progressing to the next batched iterator is illegal behavior.
	for batch := range qSet.BatchList(ctx, &client, 10) {
		go func() { _ = processExampleBatch(batch) }()
	}
}

func processExampleBatch(batch iter.Seq2[*ExampleResource, error]) error {
	for res, err := range batch {
		if err != nil {
			return err
		}

		_ = res
	}

	return nil
}
