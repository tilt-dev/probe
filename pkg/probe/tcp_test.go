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
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTCPSocket(t *testing.T) {
	// Setup a test server that responds to probing correctly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	tHost, tPortStr, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	tPort, err := strconv.Atoi(tPortStr)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	tests := []struct {
		host string
		port int

		expectedStatus Result
		expectedError  error
	}{
		// A connection is made and probing would succeed
		{tHost, tPort, Success, nil},
		// No connection can be made and probing would fail
		{tHost, -1, Failure, nil},
	}

	for i, tt := range tests {
		p := NewTCPSocket(tt.host, tt.port)
		assert.Equal(t, net.JoinHostPort(tt.host, strconv.Itoa(tt.port)), p.Address())

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		status, _, err := p.Execute(ctx)
		if status != tt.expectedStatus {
			t.Errorf("#%d: expected status=%v, get=%v", i, tt.expectedStatus, status)
		}
		if err != tt.expectedError {
			t.Errorf("#%d: expected error=%v, get=%v", i, tt.expectedError, err)
		}
		cancel()
	}
}
