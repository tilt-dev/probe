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

import (
	"context"
	"net"
	"strconv"

	"k8s.io/klog/v2"
)

// NewTCPSocket creates a TCP socket probe.
func NewTCPSocket(host string, port int) TCPSocket {
	return TCPSocket{
		addr: net.JoinHostPort(host, strconv.Itoa(port)),
	}
}

type TCPSocket struct {
	addr string
}

var _ Probe = &TCPSocket{}

// Address returns the network address being TCP probed.
func (t TCPSocket) Address() string {
	return t.addr
}

// Execute checks that a TCPSocket socket to the address can be opened.
// If the socket can be opened, it returns Success
// If the socket fails to open, it returns Failure.
func (t TCPSocket) Execute(ctx context.Context) (Result, string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", t.addr)
	if err != nil {
		// Convert errors to failures to handle timeouts.
		return Failure, err.Error(), nil
	}
	err = conn.Close()
	if err != nil {
		klog.Errorf("Unexpected error closing TCPSocket probe socket: %v (%#v)", err, err)
	}
	return Success, "", nil
}
