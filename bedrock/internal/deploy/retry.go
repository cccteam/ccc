// retry.go is the deploy steps' patience with an API whose answer can only be transient.
// A call that the API answers 5xx, or that gets no answer because the request timed out
// or its connection was reset, refused or closed early, is sent again after a wait that
// doubles each time, up to retryAttempts attempts in all; each attempt sent again is
// noted on the step's output, and the failure that ends the attempts says how many were
// made. A refusal (4xx) is the API's answer and is never sent again, nor is anything
// once the build's context is canceled.

package deploy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/go-playground/errors/v5"
)

const (
	// retryAttempts is how many times a call is sent at most; retryFirstWait is the wait
	// before the second attempt, doubled before each one after it: 5, 10, 20 and 40
	// seconds, 75 seconds of waiting before the fifth and last attempt.
	retryAttempts  = 5
	retryFirstWait = 5 * time.Second
)

// retried is a call's failure the retrier can judge: whether sending the call again may
// cure it, and, when the attempts are used up, how many were made, for its message.
type retried interface {
	error
	// transient reports whether the failure can only be transient, so the call is worth
	// sending again.
	transient() bool
	// givenUpAfter marks the failure as the last of attempts attempts, so its message
	// says so.
	givenUpAfter(attempts int)
}

// retrier sends a call again when its failure can only be transient. sleep waits between
// attempts, for real when nil; out takes the note on each attempt sent again, nowhere
// when nil.
type retrier struct {
	sleep SleepFunc
	out   io.Writer
}

// do sends the call until it answers, fails for a reason that is not transient, or the
// attempts are used up. call answers nil when the call succeeded, else the failure of
// that attempt: one that is transient (retried) is noted on out and the call is sent
// again after the wait; any other is answered as it is. The last attempt's failure
// says how many attempts were made.
func (r retrier) do(ctx context.Context, call func() error) error {
	wait := retryFirstWait
	for attempt := 1; ; attempt++ {
		err := call()
		var failure retried
		if err == nil || ctx.Err() != nil || !errors.As(err, &failure) || !failure.transient() {
			return err
		}
		if attempt == retryAttempts {
			failure.givenUpAfter(attempt)

			return err
		}
		fmt.Fprintf(r.writer(), "%s; attempt %d of %d in %s.\n", strings.TrimSuffix(failure.Error(), "."), attempt+1, retryAttempts, wait)
		if err := r.wait(ctx, wait); err != nil {
			return errors.Wrapf(err, "before sending the call again after: %s", failure)
		}
		wait *= 2
	}
}

// writer is where the notes go.
func (r retrier) writer() io.Writer {
	if r.out == nil {
		return io.Discard
	}

	return r.out
}

// wait waits for d with the retrier's sleep, or for real when none was set.
func (r retrier) wait(ctx context.Context, d time.Duration) error {
	if r.sleep == nil {
		return sleepFor(ctx, d)
	}

	return r.sleep(ctx, d)
}

// transportError is a request the API gave no answer to: the connection could not be
// made, the request timed out, or the connection was reset or closed before the answer
// was read. It is its own cause, so the message a person reads names the call.
type transportError struct {
	service      string
	method, path string
	err          error
	// attempts is how many attempts were made when this one ended them; zero or one
	// says nothing of attempts.
	attempts int
}

func (e *transportError) Error() string {
	s := fmt.Sprintf("%s did not answer %s %s: %v", e.service, e.method, e.path, withoutURL(e.err))
	if e.attempts > 1 {
		s += fmt.Sprintf("; given up after %d attempts", e.attempts)
	}

	return s
}

// transient reports a failure of the connection that can only be transient: the request
// timed out, or the connection was reset, refused or closed before the answer was read.
// A canceled request is the build's own doing, never transient. (A request that ran
// out of time reads as a deadline exceeded too, whether the HTTP client's own timeout
// or the build's context ended it; the retrier tells them apart by the context.)
func (e *transportError) transient() bool {
	if errors.Is(e.err, context.Canceled) {
		return false
	}
	var timeout net.Error
	if errors.As(e.err, &timeout) && timeout.Timeout() {
		return true
	}

	return errors.Is(e.err, syscall.ECONNRESET) || errors.Is(e.err, syscall.ECONNREFUSED) || errors.Is(e.err, io.ErrUnexpectedEOF) || errors.Is(e.err, io.EOF)
}

func (e *transportError) givenUpAfter(attempts int) {
	e.attempts = attempts
}

// withoutURL is the request's error without the method and URL the HTTP client prefixes
// it with (Get "https://…": …), which the message names already.
func withoutURL(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}

	return err
}
