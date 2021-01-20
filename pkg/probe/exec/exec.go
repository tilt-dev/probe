/*
Copyright 2015 The Kubernetes Authors.
Modified 2021 Windmill Engineering.

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

package exec

import (
	"os/exec"

	"k8s.io/klog/v2"

	"github.com/tilt-dev/probe/pkg/probe"
)

const (
	// TODO(milas): consider adding back limited output
	maxReadLength = 10 * 1 << 10 // 10KB
)

type Cmd interface {
	CombinedOutput() ([]byte, error)
}

var _ Cmd = &exec.Cmd{}

type exitError interface {
	error
	ExitCode() int
}

var _ exitError = &exec.ExitError{}

// New creates a Prober.
func New() Prober {
	return execProber{}
}

// Prober is an interface defining the Probe object for container readiness/liveness checks.
type Prober interface {
	Probe(e Cmd) (probe.Result, string, error)
}

type execProber struct{}

// Probe executes a command to check the liveness/readiness of container
// from executing a command. Returns the Result status, command output, and
// errors if any.
func (pr execProber) Probe(e Cmd) (probe.Result, string, error) {
	data, err := e.CombinedOutput()

	klog.V(4).Infof("Exec probe response: %q", string(data))
	if err != nil {
		exit, ok := err.(exitError)
		if ok {
			if exit.ExitCode() == 0 {
				return probe.Success, string(data), nil
			}
			return probe.Failure, string(data), nil
		}

		return probe.Unknown, "", err
	}
	return probe.Success, string(data), nil
}
