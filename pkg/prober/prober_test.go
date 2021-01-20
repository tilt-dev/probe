package prober

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"

	"github.com/tilt-dev/probe/pkg/probe"
)

type staticProbe struct {
	result probe.Result
}

func (s *staticProbe) Execute(ctx context.Context) probe.Result {
	return s.result
}

func mockClock() (clockwork.FakeClock, Option) {
	c := clockwork.NewFakeClockAt(time.Now())
	opt := func(p *Prober) {
		p.clock = c
	}
	return c, opt
}

func TestNewProberDefaults(t *testing.T) {
	testProbe := &staticProbe{probe.Success}
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
	testProbe := &staticProbe{probe.Success}
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

	p := NewProber(&staticProbe{probe.Success},
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

func (s *sleepProbe) Execute(ctx context.Context) probe.Result {
	select {
	case <-ctx.Done():
		return probe.Unknown
	case <-time.After(s.duration):
		s.finished = true
		return probe.Success
	}
}

func TestTimeout(t *testing.T) {
	c, withMockClock := mockClock()
	sleepProbe := &sleepProbe{duration: 5 * time.Minute}
	p := NewProber(sleepProbe, withMockClock, WithTimeout(30*time.Second))

	go p.Run(context.Background())

	c.Advance(30 * time.Second)
	c.BlockUntil(1)

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
