package generation

import (
	"reflect"
	"runtime"
	"sync"

	"github.com/go-playground/errors/v5"
)

func forEachGo[T any](s []*T, fn func(*T) error) error {
	var (
		wg      sync.WaitGroup
		errChan = make(chan error)
	)
	for _, e := range s {
		wg.Go(func() {
			if err := fn(e); err != nil {
				errChan <- err
			}
		})
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	var errs error
	for err := range errChan {
		errs = errors.Join(errs, err)
	}

	if errs != nil {
		return errors.Wrap(errs, runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name())
	}

	return nil
}
