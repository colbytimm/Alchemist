// Package writers runs the item writes of one step of a client-side write
// job through a bounded number of goroutines, and slows the job down when
// the backend throttles it. It knows nothing of what is written: every write
// is a function.
package writers

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/colbytimm/alchemist/internal/adapter"
)

const (
	DefaultSize = 4
	MaxSize     = 16
	// MaxThrottles is how many throttles in a row one write may meet before
	// it ends its step.
	MaxThrottles = 10
)

// defaultRetryAfter is the pause after a throttle that named no delay.
const defaultRetryAfter = time.Second

// ErrNotStarted is the outcome of a write its step ended before starting.
var ErrNotStarted = errors.New("writers: not started: the step ended first")

type Write func(ctx context.Context) (requestCharge float64, err error)

type Outcome struct {
	RequestCharge float64
	Throttles     int
	Err           error
}

type Clock interface {
	After(d time.Duration) <-chan time.Time
}

type SystemClock struct{}

func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Pool runs the writes of one step through a bounded number of goroutines.
// Its size only ever steps down, and it carries that from step to step.
type Pool struct {
	clock Clock

	mu   sync.Mutex
	size int
}

func NewPool(size int, clock Clock) *Pool {
	return &Pool{size: min(max(size, 1), MaxSize), clock: clock}
}

func (p *Pool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.size
}

// stepDown takes one writer away, and reports false when only one is left.
func (p *Pool) stepDown() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.size == 1 {
		return false
	}
	p.size--
	return true
}

// Run returns when every write has an outcome, in the order given. A write
// that fails with anything but a throttle, or meets MaxThrottles throttles
// in a row, ends the step: writes not yet started get ErrNotStarted and are
// never called. A throttle pauses every writer of the step for as long as
// the backend asked, and the pool loses a writer for good. Cancelling ctx
// starts no new write; writes in flight finish or fail on their own.
func (p *Pool) Run(ctx context.Context, writes []Write) []Outcome {
	outcomes := make([]Outcome, len(writes))
	s := &step{pool: p, slots: make(chan struct{}, p.Size())}
	var wg sync.WaitGroup
	for i, write := range writes {
		if !s.admit(ctx) {
			for j := i; j < len(writes); j++ {
				outcomes[j].Err = ErrNotStarted
			}
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer s.release()
			outcomes[i] = s.perform(ctx, write)
		}()
	}
	wg.Wait()
	return outcomes
}

// step is one Run. slots is the semaphore, sized to the pool when the step
// began; retired counts the slots a step-down has claimed and not yet taken
// out of use, which happens as their writers finish.
type step struct {
	pool    *Pool
	slots   chan struct{}
	retired atomic.Int32
	ended   atomic.Bool
	gate    gate
}

// admit waits for a free slot, and reports false once the step has ended
// or ctx is done.
func (s *step) admit(ctx context.Context) bool {
	if s.ended.Load() || ctx.Err() != nil {
		return false
	}
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	if s.ended.Load() || ctx.Err() != nil {
		<-s.slots
		return false
	}
	return true
}

func (s *step) release() {
	for {
		retired := s.retired.Load()
		if retired == 0 {
			<-s.slots
			return
		}
		if s.retired.CompareAndSwap(retired, retired-1) {
			return
		}
	}
}

func (s *step) perform(ctx context.Context, write Write) Outcome {
	var outcome Outcome
	for {
		if err := s.gate.wait(ctx); err != nil {
			return s.end(outcome, err)
		}
		charge, err := write(ctx)
		outcome.RequestCharge += charge
		var throttled *adapter.ThrottledError
		if !errors.As(err, &throttled) {
			if err != nil {
				return s.end(outcome, err)
			}
			return outcome
		}
		outcome.Throttles++
		if outcome.Throttles >= MaxThrottles {
			return s.end(outcome, err)
		}
		if s.pool.stepDown() {
			s.retired.Add(1)
		}
		if err := s.gate.hold(ctx, s.pool.clock, retryAfter(throttled)); err != nil {
			return s.end(outcome, err)
		}
	}
}

func (s *step) end(outcome Outcome, err error) Outcome {
	s.ended.Store(true)
	outcome.Err = err
	return outcome
}

func retryAfter(throttled *adapter.ThrottledError) time.Duration {
	if throttled.RetryAfter <= 0 {
		return defaultRetryAfter
	}
	return throttled.RetryAfter
}

// gate holds every writer of a step while one of them waits out a throttle.
type gate struct {
	mu     sync.Mutex
	paused chan struct{} // closed when the pause ends; nil while open
}

func (g *gate) wait(ctx context.Context) error {
	g.mu.Lock()
	paused := g.paused
	g.mu.Unlock()
	if paused == nil {
		return ctx.Err()
	}
	select {
	case <-paused:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// hold closes the gate for d, or waits for the pause already under way.
func (g *gate) hold(ctx context.Context, clock Clock, d time.Duration) error {
	g.mu.Lock()
	if g.paused != nil {
		g.mu.Unlock()
		return g.wait(ctx)
	}
	paused := make(chan struct{})
	g.paused = paused
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.paused = nil
		g.mu.Unlock()
		close(paused)
	}()
	select {
	case <-clock.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
