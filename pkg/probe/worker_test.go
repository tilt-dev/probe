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
}

func (s *staticProbe) Execute(_ context.Context) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result
}

func (s *staticProbe) setResult(result Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result = result
}

func newStaticProbe(result Result) *staticProbe {
	return &staticProbe{result: result}
}

func mockClock() (clockwork.FakeClock, WorkerOption) {
	c := clockwork.NewFakeClockAt(time.Now())
	opt := func(p *Worker) {
		p.clock = c
	}
	return c, opt
}

func resultsChan() (chan Result, WorkerOption) {
	results := make(chan Result)
	opt := func(p *Worker) {
		p.resultsChan = results
	}
	return results, opt
}

func TestNewProberDefaults(t *testing.T) {
	testProbe := newStaticProbe(Success)
	w := NewWorker(testProbe)
	assert.NotNil(t, w)
	assert.Equal(t, testProbe, w.probe)
	assert.Equal(t, DefaultProbePeriod, w.period)
	assert.Equal(t, DefaultProbeTimeout, w.timeout)
	assert.Equal(t, DefaultProbeSuccessThreshold, w.successThreshold)
	assert.Equal(t, DefaultProbeFailureThreshold, w.failureThreshold)
	assert.Equal(t, DefaultInitialDelay, w.initialDelay)
	assert.Nil(t, w.statusFunc)
	assert.Equal(t, w.Status(), Unknown)
}

func TestNewProberOptions(t *testing.T) {
	testProbe := newStaticProbe(Success)
	t.Run("WorkerPeriod", func(t *testing.T) {
		w := NewWorker(testProbe, WorkerPeriod(5*time.Minute))
		assert.Equal(t, 5*time.Minute, w.period)
	})

	t.Run("WorkerTimeout", func(t *testing.T) {
		w := NewWorker(testProbe, WorkerTimeout(2*time.Hour))
		assert.Equal(t, 2*time.Hour, w.timeout)
	})

	t.Run("WorkerInitialDelay", func(t *testing.T) {
		w := NewWorker(testProbe, WorkerInitialDelay(500*time.Millisecond))
		assert.Equal(t, 500*time.Millisecond, w.initialDelay)
	})

	t.Run("WorkerSuccessThreshold", func(t *testing.T) {
		w := NewWorker(testProbe, WorkerSuccessThreshold(1000))
		assert.Equal(t, 1000, w.successThreshold)
	})

	t.Run("WorkerFailureThreshold", func(t *testing.T) {
		w := NewWorker(testProbe, WorkerFailureThreshold(99))
		assert.Equal(t, 99, w.failureThreshold)
	})

	t.Run("WorkerTimeout", func(t *testing.T) {
		called := false
		statusFunc := func(status Result) {
			called = true
		}

		w := NewWorker(testProbe, WorkerOnStatusChange(statusFunc))
		assert.NotNil(t, w.statusFunc)
		w.statusFunc(Success)
		assert.True(t, called)
	})
}

func TestInitialDelay(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	statusFunc := func(status Result) {
		wg.Done()
	}

	c, withMockClock := mockClock()

	w := NewWorker(newStaticProbe(Success),
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

type sleepProbe struct {
	finished bool
	duration time.Duration
}

func (s *sleepProbe) Execute(ctx context.Context) Result {
	select {
	case <-ctx.Done():
		return Unknown
	case <-time.After(s.duration):
		s.finished = true
		return Success
	}
}

func TestTimeout(t *testing.T) {
	_, withMockClock := mockClock()
	results, withResultsChan := resultsChan()

	sleepProbe := &sleepProbe{duration: 5 * time.Minute}
	w := NewWorker(sleepProbe, withMockClock, withResultsChan, WorkerTimeout(5*time.Millisecond))

	go w.Run(context.Background())

	// ensure that a result was received but that it did NOT come from our probe
	<-results
	assert.False(t, sleepProbe.finished)
	assert.Equal(t, Failure, w.Status())
}

func TestStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sleepProbe := &sleepProbe{duration: 5 * time.Minute}
	w := NewWorker(sleepProbe)
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
			w := NewWorker(staticProbe, opts...)

			go w.Run(context.Background())

			// wait for initial status to process
			assert.Equal(t, initialStatus, <-results)
			staticProbe.setResult(tc.expectedStatus)

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
