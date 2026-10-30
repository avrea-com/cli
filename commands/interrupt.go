package commands

import (
	"context"
	"os"
	"os/signal"
	"sync/atomic"
)

type interruptPolicyKey struct{}
type interruptPolicy struct{ childOwnsInterrupt atomic.Bool }

// InterruptContext is for executable entrypoints. Commands normally cancel on
// Ctrl-C; an attached interactive SSH child owns terminal interrupts until it
// exits. External cancellation of parent always remains effective.
func InterruptContext(parent context.Context) (context.Context, context.CancelFunc) {
	base, cancel := context.WithCancel(parent)
	policy := &interruptPolicy{}
	ctx := context.WithValue(base, interruptPolicyKey{}, policy)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	go func() {
		defer signal.Stop(signals)
		for {
			select {
			case <-base.Done():
				return
			case <-signals:
				if !policy.childOwnsInterrupt.Load() {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, func() { signal.Stop(signals); cancel() }
}
func sshOwnsInterrupt(ctx context.Context) func() {
	policy, _ := ctx.Value(interruptPolicyKey{}).(*interruptPolicy)
	if policy == nil {
		return func() {}
	}
	before := policy.childOwnsInterrupt.Swap(true)
	return func() { policy.childOwnsInterrupt.Store(before) }
}
