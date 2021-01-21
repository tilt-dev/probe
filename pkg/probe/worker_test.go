package probe

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticProbe struct {
	mu     sync.Mutex
	result Result
	output string
	err    error
}

func (s *staticProbe) probe(_ context.Context) (Result, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result, s.output, s.err
}

func (s *staticProbe) update(result Result, output string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result = result
	s.output = output
	s.err = err
}

func newStaticProbe(result Result, output string, err error) *staticProbe {
	return &staticProbe{result: result, output: output, err: err}
}

func mockClock() (clockwork.FakeClock, WorkerOption) {
	c := clockwork.NewFakeClockAt(time.Now())
	opt := func(p *Worker) {
		p.clock = c
	}
	return c, opt
}

func resultsChan() (chan probeResult, WorkerOption) {
	results := make(chan probeResult)
	opt := func(p *Worker) {
		p.resultsChan = results
	}
	return results, opt
}

func TestNewProberDefaults(t *testing.T) {
	testProbe := newStaticProbe(Success, "", nil)
	w := NewWorker(testProbe.probe)
	assert.NotNil(t, w)
	assert.NotNil(t, w.probe)
	assert.Equal(t, DefaultProbePeriod, w.period)
	assert.Equal(t, DefaultProbeTimeout, w.timeout)
	assert.Equal(t, DefaultProbeSuccessThreshold, w.successThreshold)
	assert.Equal(t, DefaultProbeFailureThreshold, w.failureThreshold)
	assert.Equal(t, DefaultInitialDelay, w.initialDelay)
	assert.Nil(t, w.statusFunc)
	assert.Equal(t, w.Status(), Unknown)
}

func TestNewProberOptions(t *testing.T) {
	testProbe := newStaticProbe(Success, "test output", nil)
	t.Run("WorkerPeriod", func(t *testing.T) {
		w := NewWorker(testProbe.probe, WorkerPeriod(5*time.Minute))
		assert.Equal(t, 5*time.Minute, w.period)
	})

	t.Run("WorkerTimeout", func(t *testing.T) {
		w := NewWorker(testProbe.probe, WorkerTimeout(2*time.Hour))
		assert.Equal(t, 2*time.Hour, w.timeout)
	})

	t.Run("WorkerInitialDelay", func(t *testing.T) {
		w := NewWorker(testProbe.probe, WorkerInitialDelay(500*time.Millisecond))
		assert.Equal(t, 500*time.Millisecond, w.initialDelay)
	})

	t.Run("WorkerSuccessThreshold", func(t *testing.T) {
		w := NewWorker(testProbe.probe, WorkerSuccessThreshold(1000))
		assert.Equal(t, 1000, w.successThreshold)
	})

	t.Run("WorkerFailureThreshold", func(t *testing.T) {
		w := NewWorker(testProbe.probe, WorkerFailureThreshold(99))
		assert.Equal(t, 99, w.failureThreshold)
	})

	t.Run("WorkerOnStatusChange", func(t *testing.T) {
		called := false
		statusFunc := func(status Result, output string) {
			called = true
		}

		w := NewWorker(testProbe.probe, WorkerOnStatusChange(statusFunc))
		assert.NotNil(t, w.statusFunc)
		w.statusFunc(Success, "test output")
		assert.True(t, called)
	})
}

func TestInitialDelay(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	statusFunc := func(status Result, output string) {
		wg.Done()
	}

	c, withMockClock := mockClock()

	w := NewWorker(newStaticProbe(Success, "", nil).probe,
		WorkerOnStatusChange(statusFunc),
		WorkerInitialDelay(1*time.Minute),
		withMockClock)

	go w.Run(context.Background())
	defer w.Stop()

	c.BlockUntil(1)
	assert.Equal(t, Failure, w.Status())

	c.Advance(30 * time.Second)
	assert.Equal(t, Failure, w.Status())

	c.Advance(30 * time.Second)
	wg.Wait()
	assert.Equal(t, Success, w.Status())
}

func sleepProbe(duration time.Duration) Probe {
	return func(ctx context.Context) (Result, string, error) {
		select {
		case <-ctx.Done():
			return Unknown, "context done", nil
		case <-time.After(duration):
			return Success, "sleep finished", nil
		}
	}
}

func TestTimeout(t *testing.T) {
	_, withMockClock := mockClock()
	results, withResultsChan := resultsChan()

	w := NewWorker(sleepProbe(5*time.Minute), withMockClock, withResultsChan, WorkerTimeout(5*time.Millisecond))

	go w.Run(context.Background())

	// ensure that a result was received but that it did NOT come from our probe
	result := <-results
	assert.Equal(t, Failure, result.result)
	assert.Equal(t, "", result.output)
	assert.Equal(t, Failure, w.Status())
}

func TestStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	w := NewWorker(sleepProbe(5 * time.Minute))
	go w.Run(ctx)

	w.Stop()

	assert.Nil(t, w.stopFunc)
	assert.Equal(t, Unknown, w.Status())
}

func TestThresholds(t *testing.T) {
	type tc struct {
		expectedStatus Result
		opts           []WorkerOption
	}

	const threshold = 15

	tcs := []tc{
		{
			Success,
			[]WorkerOption{WorkerFailureThreshold(1), WorkerSuccessThreshold(threshold)},
		},
		{
			Failure,
			[]WorkerOption{WorkerFailureThreshold(threshold), WorkerSuccessThreshold(1)},
		},
	}

	for _, tc := range tcs {
		t.Run(string(tc.expectedStatus), func(t *testing.T) {
			results, withResultsChan := resultsChan()
			c, withMockClock := mockClock()
			var initialStatus Result
			if tc.expectedStatus == Success {
				initialStatus = Failure
			} else if tc.expectedStatus == Failure {
				initialStatus = Success
			} else {
				require.Fail(t, "Unsupported status: %s", tc.expectedStatus)
			}

			opts := []WorkerOption{withMockClock, withResultsChan, WorkerInitialDelay(0), WorkerPeriod(1 * time.Minute)}
			opts = append(opts, tc.opts...)
			staticProbe := &staticProbe{result: initialStatus}
			w := NewWorker(staticProbe.probe, opts...)

			go w.Run(context.Background())

			// wait for initial status to process
			assert.Equal(t, probeResult{result: initialStatus}, <-results)
			staticProbe.update(tc.expectedStatus, "", nil)

			for invocation := 1; invocation <= threshold; invocation++ {
				c.Advance(1 * time.Minute)
				c.BlockUntil(1)
				<-results
				if invocation != threshold {
					// don't actually care WHAT it is as long as it's not the end state yet
					require.NotEqualf(t, tc.expectedStatus, w.Status(), "status prematurely reached at invocation %d", invocation)
				} else {
					require.Equal(t, tc.expectedStatus, w.Status())
					break
				}
			}
		})
	}
}
