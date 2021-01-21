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
	"k8s.io/klog/v2"
	"k8s.io/utils/exec"

	"github.com/tilt-dev/probe/pkg/probe"
)

const (
	// TODO(milas): consider adding back limited output
	maxReadLength = 10 * 1 << 10 // 10KB
)

// osExecRunner is the default runner that's a shim around stdlib os/exec.
//
// A global instance is used to avoid creating an instance for every probe (it is Goroutine safe).
var osExecRunner = exec.New()

// New creates a Prober.
func New() Prober {
	return execProber{
		runner: osExecRunner,
	}
}

// Prober is an interface defining the Probe object for container readiness/liveness checks.
type Prober interface {
	Probe(name string, args ...string) (probe.Result, string, error)
}

type execProber struct {
	runner exec.Interface
}

// Probe executes a command to check the liveness/readiness of container
// from executing a command. Returns the Result status, command output, and
// errors if any.
func (pr execProber) Probe(name string, args ...string) (probe.Result, string, error) {
	cmd := pr.runner.Command(name, args...)
	data, err := cmd.CombinedOutput()

	klog.V(4).Infof("Exec probe response: %q", string(data))
	if err != nil {
		exit, ok := err.(exec.ExitError)
		if ok {
			if exit.ExitStatus() == 0 {
				return probe.Success, string(data), nil
			}
			return probe.Failure, string(data), nil
		}

		return probe.Unknown, "", err
	}
	return probe.Success, string(data), nil
}
