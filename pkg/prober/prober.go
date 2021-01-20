package prober

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"

	"github.com/tilt-dev/probe/pkg/probe"
)

const (
	// DefaultProbePeriod is the default value for the interval between
	// probe invocations.
	DefaultProbePeriod = 10 * time.Second

	// DefaultProbeTimeout is the default value for the timeout when
	// executing a probe to cancel it and consider it failed.
	DefaultProbeTimeout = 1 * time.Second

	// DefaultInitialDelay is the default value for the initial delay
	// before beginning to invoke the probe after the Prober is started.
	DefaultInitialDelay = 0 * time.Second

	// DefaultProbeSuccessThreshold is the default value for the
	// minimum number of consecutive successes required after having
	// failed before the status will transition to probe.Success.
	DefaultProbeSuccessThreshold = 1

	// DefaultProbeFailureThreshold is the default value for the
	// minimum number of consecutive failures required after having
	// succeeded before the status will transition to probe.Failure.
	DefaultProbeFailureThreshold = 3
)

var realClock = clockwork.NewRealClock()

// StatusChangedFunc is invoked on status transitions.
//
// It will NOT be called for subsequent probe invocations that do not
// result in a status change.
type StatusChangedFunc func(status probe.Result)

// Option can be passed when creating a Prober to configure the
// instance.
type Option func(w *Prober)

// NewProber creates a Prober instance using the provided probe.Probe
// and options (if any).
func NewProber(p probe.Probe, opts ...Option) *Prober {
	w := &Prober{
		probe:            p,
		clock:            realClock,
		period:           DefaultProbePeriod,
		timeout:          DefaultProbeTimeout,
		initialDelay:     DefaultInitialDelay,
		successThreshold: DefaultProbeSuccessThreshold,
		failureThreshold: DefaultProbeFailureThreshold,
		status:           probe.Unknown,
	}

	for _, opt := range opts {
		opt(w)
	}

	return w
}

// Prober handles executing a probe.Probe and reporting results.
//
// It's loosely based (but simplified) on the k8s.io/kubernetes/pkg/kubelet/prober design.
type Prober struct {
	probe probe.Probe

	clock clockwork.Clock
	mu    sync.Mutex

	stopFunc context.CancelFunc

	initialDelay time.Duration
	period       time.Duration
	timeout      time.Duration

	successThreshold int
	failureThreshold int

	// status is only updated after the failure/success threshold is crossed
	status probe.Result

	statusFunc StatusChangedFunc

	lastResult probe.Result
	resultRun  int
}

// Run periodically executes the probe until stopped.
//
// The Prober can be stopped by explicitly calling Stop() or implicitly
// via context cancellation.
//
// Calling Run() on an instance that is already running will result in
// a panic.
func (w *Prober) Run(ctx context.Context) {
	w.mu.Lock()
	if w.stopFunc != nil {
		panic("prober is already running")
	}
	ctx, cancel := context.WithCancel(ctx)
	w.stopFunc = cancel

	w.lastResult = probe.Unknown
	w.resultRun = 0
	// initial status is failure until a successful probe
	w.status = probe.Failure

	w.mu.Unlock()

	w.clock.Sleep(w.initialDelay)

	ticker := w.clock.NewTicker(w.period)
	for {
		w.doProbe(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
		}
	}
}

// Stop halts further probe invocations. It is safe to call Stop()
// more than once.
func (w *Prober) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopFunc != nil {
		w.stopFunc()
		w.stopFunc = nil
		w.status = probe.Unknown
	}
}

// Status returns the current probe result.
//
// If not running, this will always return probe.Unknown.
func (w *Prober) Status() probe.Result {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

func (w *Prober) doProbe(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()
	result := make(chan probe.Result, 1)
	go func() {
		result <- w.probe.Execute(ctx)
	}()

	for {
		select {
		case r := <-result:
			w.handleResult(r)
			return
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				// only context deadline exceeded triggers a result handling
				// (if context was explicitly canceled, there's no reason to
				// record a result as the prober is being stopped)
				w.handleResult(probe.Failure)
			}
			return
		}
	}
}

// handleResult updates prober internal state based on the probe result.
//
// This is very similar to https://github.com/kubernetes/kubernetes/blob/v1.20.2/pkg/kubelet/prober/worker.go#L260-L273
func (w *Prober) handleResult(result probe.Result) {
	if w.lastResult == result {
		w.resultRun++
	} else {
		w.lastResult = result
		w.resultRun = 1
	}

	success := isSuccessResult(result)
	if (!success && w.resultRun < w.failureThreshold) ||
		(success && w.resultRun < w.successThreshold) {
		return
	}

	w.mu.Lock()
	if w.stopFunc == nil || w.status == result {
		w.mu.Unlock()
		return
	}
	w.status = result
	w.mu.Unlock()

	if w.statusFunc != nil {
		w.statusFunc(result)
	}
}

// isSuccessResult coerces a probe.Result value into a bool based on
// whether it's considered a successful value or not.
func isSuccessResult(result probe.Result) bool {
	if result == probe.Success || result == probe.Warning {
		return true
	}
	return false
}

// WithPeriod sets the period between probe invocations.
func WithPeriod(period time.Duration) Option {
	return func(w *Prober) {
		w.period = period
	}
}

// WithTimeout sets the duration before a running probe is canceled
// and considered to have failed.
func WithTimeout(timeout time.Duration) Option {
	return func(w *Prober) {
		w.timeout = timeout
	}
}

// WithFailureThreshold sets the number of consecutive failures
// required after a probe has succeeded before the status will
// transition to probe.Failure.
func WithFailureThreshold(v int) Option {
	return func(w *Prober) {
		w.failureThreshold = v
	}
}

// WithSuccessThreshold sets the number of consecutive successes
// required after a probe has failed before the status will
// transition to probe.Success.
func WithSuccessThreshold(v int) Option {
	return func(w *Prober) {
		w.successThreshold = v
	}
}

// WithInitialDelay sets the amount of time that will be waited
// when the prober starts before beginning to invoke the probe.
//
// The status will be probe.Failure during the initial delay
// period.
func WithInitialDelay(delay time.Duration) Option {
	return func(w *Prober) {
		w.initialDelay = delay
	}
}

// WithStatusChangeFunc sets the function to invoke when the status
// transitions.
//
// Subsequent probe invocations that do not result in a change to the
// status (either because they return the same result or the failure/
// success threshold has not been met) will not emit a status change
// update.
func WithStatusChangeFunc(f StatusChangedFunc) Option {
	return func(w *Prober) {
		w.statusFunc = f
	}
}
