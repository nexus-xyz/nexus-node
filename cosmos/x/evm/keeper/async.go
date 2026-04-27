package keeper

import (
	"context"
	"time"
)

const (
	// retryDelay is the fixed sleep between retry attempts (PrepareProposal unknown-payload cap uses it).
	retryDelay     = 10 * time.Millisecond
	requestTimeout = 10 * time.Second
)

// Retry retries the given function every retryDelay until it returns true, or
// the context is done, or the total allocation of time exceeds `requestTimeout`.
//
// Errors are returned immediately; otherwise, the function retries if `ok` is false.
func retry(ctx context.Context, fn func(ctx context.Context) (bool, error)) error {
	for {
		timeoutCtx, cancel := context.WithTimeout(ctx, requestTimeout)

		ok, err := fn(timeoutCtx)
		cancel()

		if err != nil {
			return err
		}
		if ok {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		time.Sleep(retryDelay)
	}
}
