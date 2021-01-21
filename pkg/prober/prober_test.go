package prober

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tilt-dev/probe/pkg/probe"
)

type staticProbe struct {
	mu     sync.Mutex
	result probe.Result
}

func (s *staticProbe) Execute(_ context.Context) (probe.Result, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result, "", nil
}

func (s *staticProbe) setResult(result probe.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result = result
}

func newStaticProbe(result probe.Result) *staticProbe {
	return &staticProbe{result: result}
}

func mockClock() (clockwork.FakeClock, Option) {
	c := clockwork.NewFakeClockAt(time.Now())
	opt := func(p *Prober) {
		p.clock = c
	}
	return c, opt
}

func resultsChan() (chan probe.Result, Option) {
	results := make(chan probe.Result)
	opt := func(p *Prober) {
		p.resultsChan = results
	}
	return results, opt
}

func TestNewProberDefaults(t *testing.T) {
	testProbe := newStaticProbe(probe.Success)
	p := NewProber(testProbe)
	assert.NotNil(t, p)
	assert.Equal(t, testProbe, p.probe)
	assert.Equal(t, DefaultProbePeriod, p.period)
	assert.Equal(t, DefaultProbeTimeout, p.timeout)
	assert.Equal(t, DefaultProbeSuccessThreshold, p.successThreshold)
	assert.Equal(t, DefaultProbeFailureThreshold, p.failureThreshold)
	assert.Equal(t, DefaultInitialDelay, p.initialDelay)
	assert.Nil(t, p.statusFunc)
	assert.Equal(t, p.Status(), probe.Unknown)
}

func TestNewProberOptions(t *testing.T) {
	testProbe := newStaticProbe(probe.Success)
	t.Run("WithPeriod", func(t *testing.T) {
		p := NewProber(testProbe, WithPeriod(5*time.Minute))
		assert.Equal(t, 5*time.Minute, p.period)
	})

	t.Run("WithTimeout", func(t *testing.T) {
		p := NewProber(testProbe, WithTimeout(2*time.Hour))
		assert.Equal(t, 2*time.Hour, p.timeout)
	})

	t.Run("WithInitialDelay", func(t *testing.T) {
		p := NewProber(testProbe, WithInitialDelay(500*time.Millisecond))
		assert.Equal(t, 500*time.Millisecond, p.initialDelay)
	})

	t.Run("WithSuccessThreshold", func(t *testing.T) {
		p := NewProber(testProbe, WithSuccessThreshold(1000))
		assert.Equal(t, 1000, p.successThreshold)
	})

	t.Run("WithFailureThreshold", func(t *testing.T) {
		p := NewProber(testProbe, WithFailureThreshold(99))
		assert.Equal(t, 99, p.failureThreshold)
	})

	t.Run("WithTimeout", func(t *testing.T) {
		called := false
		statusFunc := func(status probe.Result) {
			called = true
		}

		p := NewProber(testProbe, WithStatusChangeFunc(statusFunc))
		assert.NotNil(t, p.statusFunc)
		p.statusFunc(probe.Success)
		assert.True(t, called)
	})
}

func TestInitialDelay(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	statusFunc := func(status probe.Result) {
		wg.Done()
	}

	c, withMockClock := mockClock()

	p := NewProber(newStaticProbe(probe.Success),
		WithStatusChangeFunc(statusFunc),
		WithInitialDelay(1*time.Minute),
		withMockClock)

	go p.Run(context.Background())
	defer p.Stop()

	c.BlockUntil(1)
	assert.Equal(t, probe.Failure, p.Status())

	c.Advance(30 * time.Second)
	assert.Equal(t, probe.Failure, p.Status())

	c.Advance(30 * time.Second)
	wg.Wait()
	assert.Equal(t, probe.Success, p.Status())
}

type sleepProbe struct {
	finished bool
	duration time.Duration
}

func (s *sleepProbe) Execute(ctx context.Context) (probe.Result, string, error) {
	select {
	case <-ctx.Done():
		return probe.Unknown, "context done", nil
	case <-time.After(s.duration):
		s.finished = true
		return probe.Success, "", nil
	}
}

func TestTimeout(t *testing.T) {
	_, withMockClock := mockClock()
	results, withResultsChan := resultsChan()

	sleepProbe := &sleepProbe{duration: 5 * time.Minute}
	p := NewProber(sleepProbe, withMockClock, withResultsChan, WithTimeout(5*time.Millisecond))

	go p.Run(context.Background())

	// ensure that a result was received but that it did NOT come from our probe
	<-results
	assert.False(t, sleepProbe.finished)
	assert.Equal(t, probe.Failure, p.Status())
}

func TestStop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sleepProbe := &sleepProbe{duration: 5 * time.Minute}
	p := NewProber(sleepProbe)
	go p.Run(ctx)

	p.Stop()

	assert.Nil(t, p.stopFunc)
	assert.Equal(t, probe.Unknown, p.Status())
}

func TestThresholds(t *testing.T) {
	type tc struct {
		expectedStatus probe.Result
		opts           []Option
	}

	const threshold = 15

	tcs := []tc{
		{
			probe.Success,
			[]Option{WithFailureThreshold(1), WithSuccessThreshold(threshold)},
		},
		{
			probe.Failure,
			[]Option{WithFailureThreshold(threshold), WithSuccessThreshold(1)},
		},
	}

	for _, tc := range tcs {
		t.Run(string(tc.expectedStatus), func(t *testing.T) {
			results, withResultsChan := resultsChan()
			c, withMockClock := mockClock()
			var initialStatus probe.Result
			if tc.expectedStatus == probe.Success {
				initialStatus = probe.Failure
			} else if tc.expectedStatus == probe.Failure {
				initialStatus = probe.Success
			} else {
				require.Fail(t, "Unsupported status: %s", tc.expectedStatus)
			}

			opts := []Option{withMockClock, withResultsChan, WithInitialDelay(0), WithPeriod(1 * time.Minute)}
			opts = append(opts, tc.opts...)
			staticProbe := &staticProbe{result: initialStatus}
			p := NewProber(staticProbe, opts...)

			go p.Run(context.Background())

			// wait for initial status to process
			assert.Equal(t, initialStatus, <-results)
			staticProbe.setResult(tc.expectedStatus)

			for invocation := 1; invocation <= threshold; invocation++ {
				c.Advance(1 * time.Minute)
				c.BlockUntil(1)
				<-results
				if invocation != threshold {
					// don't actually care WHAT it is as long as it's not the end state yet
					require.NotEqualf(t, tc.expectedStatus, p.Status(), "status prematurely reached at invocation %d", invocation)
				} else {
					require.Equal(t, tc.expectedStatus, p.Status())
					break
				}
			}
		})
	}
}
