/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package probe

import "context"

// Result is a string used to handle the results for probing container readiness/liveness
type Result string

const (
	// Success Result
	Success Result = "success"
	// Warning Result. Logically success, but with additional debugging information attached.
	Warning Result = "warning"
	// Failure Result
	Failure Result = "failure"
	// Unknown Result
	Unknown Result = "unknown"
)

// Prober performs a check to determine a service status.
type Prober interface {
	// Probe executes a single status check.
	//
	// result is the current service status
	// output is optional info from the probe (such as a process stdout or HTTP response)
	// err indicates an issue with the probe itself and that the result should be ignored
	Probe(ctx context.Context) (result Result, output string, err error)
}

// ProberFunc is a functional version of Prober.
type ProberFunc func(ctx context.Context) (Result, string, error)

// Probe executes a single status check.
//
// result is the current service status
// output is optional info from the probe (such as a process stdout or HTTP response)
// err indicates an issue with the probe itself and that the result should be ignored
func (f ProberFunc) Probe(ctx context.Context) (Result, string, error) {
	return f(ctx)
}
