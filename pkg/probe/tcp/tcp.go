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

package tcp

import (
	"context"
	"net"
	"strconv"

	"k8s.io/klog/v2"

	"github.com/tilt-dev/probe/pkg/probe"
)

// New creates Prober.
func New() Prober {
	return tcpProber{}
}

// Prober is an interface that defines the Probe function for doing TCP readiness/liveness checks.
type Prober interface {
	Probe(ctx context.Context, host string, port int) (probe.Result, string, error)
}

type tcpProber struct{}

// Probe returns a ProbeRunner capable of running an TCP check.
func (pr tcpProber) Probe(ctx context.Context, host string, port int) (probe.Result, string, error) {
	return doTCPProbe(ctx, net.JoinHostPort(host, strconv.Itoa(port)))
}

// doTCPProbe checks that a TCP socket to the address can be opened.
// If the socket can be opened, it returns Success
// If the socket fails to open, it returns Failure.
func doTCPProbe(ctx context.Context, addr string) (probe.Result, string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		// Convert errors to failures to handle timeouts.
		return probe.Failure, err.Error(), nil
	}
	err = conn.Close()
	if err != nil {
		klog.Errorf("Unexpected error closing TCP probe socket: %v (%#v)", err, err)
	}
	return probe.Success, "", nil
}
