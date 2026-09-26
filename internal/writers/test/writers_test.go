package writers_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter"
	"github.com/colbytimm/alchemist/internal/writers"
)

// sizes are the pool sizes every behavior is checked at: one writer, where
// the semaphore alone orders everything, and several, where it does not.
var sizes = []int{1, 8}

func eachSize(t *testing.T, test func(t *testing.T, size int)) {
	t.Helper()
	for _, size := range sizes {
		t.Run(fmt.Sprintf("size %d", size), func(t *testing.T) { test(t, size) })
	}
}

// recordingClock answers every wait at once and keeps what was asked for.
type recordingClock struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (c *recordingClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, d)
	fired := make(chan time.Time, 1)
	fired <- time.Time{}
	return fired
}

func (c *recordingClock) asked() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// manualClock lets every wait end only when the test closes fire, and
// closes began when the first wait starts.
type manualClock struct {
	once  sync.Once
	began chan struct{}
	fire  chan time.Time
}

func (c *manualClock) After(time.Duration) <-chan time.Time {
	c.once.Do(func() { close(c.began) })
	return c.fire
}

func throttle(retryAfter time.Duration) error {
	return &adapter.ThrottledError{RetryAfter: retryAfter, Err: errors.New("429")}
}

// throttledTimes fails its first n calls as throttled.
func throttledTimes(n int, retryAfter time.Duration, charge float64) writers.Write {
	var calls atomic.Int32
	return func(context.Context) (float64, error) {
		if int(calls.Add(1)) <= n {
			return 0, throttle(retryAfter)
		}
		return charge, nil
	}
}

func costing(charge float64) writers.Write {
	return func(context.Context) (float64, error) { return charge, nil }
}

func TestOutcomesComeBackInTheOrderGiven(t *testing.T) {
	eachSize(t, func(t *testing.T, size int) {
		var writes []writers.Write
		for i := range 20 {
			writes = append(writes, costing(float64(i)))
		}

		outcomes := writers.NewPool(size, &recordingClock{}).Run(context.Background(), writes)

		require.Len(t, outcomes, 20)
		for i, outcome := range outcomes {
			require.NoError(t, outcome.Err)
			assert.InDelta(t, float64(i), outcome.RequestCharge, 0)
		}
	})
}

func TestNoMoreWritesRunAtOnceThanThePoolHolds(t *testing.T) {
	eachSize(t, func(t *testing.T, size int) {
		var inFlight, highest atomic.Int32
		write := func(context.Context) (float64, error) {
			now := inFlight.Add(1)
			for {
				seen := highest.Load()
				if now <= seen || highest.CompareAndSwap(seen, now) {
					break
				}
			}
			runtime.Gosched()
			inFlight.Add(-1)
			return 1, nil
		}
		writes := make([]writers.Write, 64)
		for i := range writes {
			writes[i] = write
		}

		writers.NewPool(size, &recordingClock{}).Run(context.Background(), writes)

		assert.LessOrEqual(t, int(highest.Load()), size)
		assert.Positive(t, highest.Load())
	})
}

func TestNewPoolClampsItsSize(t *testing.T) {
	tests := []struct {
		name string
		size int
		want int
	}{
		{name: "zero is one", size: 0, want: 1},
		{name: "negative is one", size: -3, want: 1},
		{name: "in range is kept", size: writers.DefaultSize, want: writers.DefaultSize},
		{name: "past the maximum is the maximum", size: 40, want: writers.MaxSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, writers.NewPool(tt.size, writers.SystemClock{}).Size())
		})
	}
}

func TestAThrottleIsWaitedOutAndRetried(t *testing.T) {
	tests := []struct {
		name       string
		retryAfter time.Duration
		wantWait   time.Duration
	}{
		{name: "for the delay the backend named", retryAfter: 3 * time.Second, wantWait: 3 * time.Second},
		{name: "for a second when it named none", retryAfter: 0, wantWait: time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eachSize(t, func(t *testing.T, size int) {
				clock := &recordingClock{}

				outcomes := writers.NewPool(size, clock).Run(context.Background(),
					[]writers.Write{throttledTimes(1, tt.retryAfter, 5)})

				require.NoError(t, outcomes[0].Err)
				assert.Equal(t, 1, outcomes[0].Throttles)
				assert.InDelta(t, 5, outcomes[0].RequestCharge, 0)
				assert.Equal(t, []time.Duration{tt.wantWait}, clock.asked())
			})
		})
	}
}

func TestEachThrottleTakesAWriterAwayForGood(t *testing.T) {
	eachSize(t, func(t *testing.T, size int) {
		pool := writers.NewPool(size, &recordingClock{})

		pool.Run(context.Background(), []writers.Write{throttledTimes(1, 0, 1)})
		afterOne := pool.Size()
		pool.Run(context.Background(), []writers.Write{costing(1), costing(1)})
		pool.Run(context.Background(), []writers.Write{throttledTimes(3, 0, 1)})

		assert.Equal(t, max(size-1, 1), afterOne, "the next Run starts from the smaller size")
		assert.Equal(t, max(size-4, 1), pool.Size(), "and it never goes below one")
	})
}

// TestAThrottleHoldsBackEveryWriter throttles a second write while the
// first one's pause is under way: neither is retried before the pause ends.
func TestAThrottleHoldsBackEveryWriter(t *testing.T) {
	clock := &manualClock{began: make(chan struct{}), fire: make(chan time.Time)}
	var fired atomic.Bool
	var early atomic.Int32
	retried := func(n int) writers.Write {
		var calls atomic.Int32
		return func(context.Context) (float64, error) {
			switch calls.Add(1) {
			case 1:
				if n == 1 {
					<-clock.began
				}
				return 0, throttle(0)
			default:
				if !fired.Load() {
					early.Add(1)
				}
				return 1, nil
			}
		}
	}
	pool := writers.NewPool(2, clock)

	done := make(chan []writers.Outcome)
	go func() { done <- pool.Run(context.Background(), []writers.Write{retried(0), retried(1)}) }()
	<-clock.began
	fired.Store(true)
	close(clock.fire)
	outcomes := <-done

	require.NoError(t, outcomes[0].Err)
	require.NoError(t, outcomes[1].Err)
	assert.Zero(t, early.Load(), "a write was retried before the pause ended")
	assert.Equal(t, 1, pool.Size())
}

func TestTooManyThrottlesInARowEndTheStep(t *testing.T) {
	eachSize(t, func(t *testing.T, size int) {
		var called [20]atomic.Bool
		writes := []writers.Write{throttledTimes(writers.MaxThrottles, 0, 1)}
		for i := 1; i < len(called); i++ {
			writes = append(writes, func(context.Context) (float64, error) {
				called[i].Store(true)
				return 1, nil
			})
		}

		outcomes := writers.NewPool(size, &recordingClock{}).Run(context.Background(), writes)

		var throttled *adapter.ThrottledError
		require.ErrorAs(t, outcomes[0].Err, &throttled)
		assert.Equal(t, writers.MaxThrottles, outcomes[0].Throttles)
		notStarted := 0
		for i, outcome := range outcomes[1:] {
			if errors.Is(outcome.Err, writers.ErrNotStarted) {
				notStarted++
				assert.False(t, called[i+1].Load(), "write %d was reported not started, yet called", i+1)
			}
		}
		if size == 1 {
			assert.Equal(t, len(called)-1, notStarted, "one writer starts nothing after the write that ended the step")
		}
	})
}

func TestAFailureEndsTheStep(t *testing.T) {
	eachSize(t, func(t *testing.T, size int) {
		refused := errors.New("403 Forbidden")
		var laterCalls atomic.Int32
		writes := []writers.Write{func(context.Context) (float64, error) { return 1, refused }}
		for range 30 {
			writes = append(writes, func(context.Context) (float64, error) {
				laterCalls.Add(1)
				return 1, nil
			})
		}

		outcomes := writers.NewPool(size, &recordingClock{}).Run(context.Background(), writes)

		require.ErrorIs(t, outcomes[0].Err, refused)
		notStarted := 0
		for _, outcome := range outcomes {
			if errors.Is(outcome.Err, writers.ErrNotStarted) {
				notStarted++
			}
		}
		assert.Equal(t, 30, int(laterCalls.Load())+notStarted, "every later write was either called or reported not started")
		if size == 1 {
			assert.Zero(t, laterCalls.Load())
		}
	})
}

func TestACancelledContextStartsNoWrite(t *testing.T) {
	eachSize(t, func(t *testing.T, size int) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var calls atomic.Int32
		write := func(context.Context) (float64, error) {
			calls.Add(1)
			return 1, nil
		}

		outcomes := writers.NewPool(size, &recordingClock{}).Run(ctx, []writers.Write{write, write, write})

		assert.Zero(t, calls.Load())
		for _, outcome := range outcomes {
			assert.ErrorIs(t, outcome.Err, writers.ErrNotStarted)
		}
	})
}

func TestCancellingMidStepLetsWritesInFlightFinish(t *testing.T) {
	eachSize(t, func(t *testing.T, size int) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var calls atomic.Int32
		writes := make([]writers.Write, 3*size)
		for i := range writes {
			writes[i] = func(context.Context) (float64, error) {
				if calls.Add(1) == 1 {
					cancel()
				}
				return 1, nil
			}
		}

		outcomes := writers.NewPool(size, &recordingClock{}).Run(ctx, writes)

		started := 0
		for _, outcome := range outcomes {
			if outcome.Err == nil {
				started++
			}
		}
		assert.Equal(t, int(calls.Load()), started, "every write that started finished")
		assert.LessOrEqual(t, started, size, "none started after the cancel")
	})
}
