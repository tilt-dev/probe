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

package prober

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewExecProber(t *testing.T) {
	prober := NewExecProber()
	execProber := prober.(execProber)
	assert.NotNil(t, execProber.excer)
}

func TestExec(t *testing.T) {
	// NOTE(milas): this test case is faulty (and is in upstream k8s - the mock doesn't properly use the input)
	// tenKilobyte := strings.Repeat("logs-123", 128*10)      // 8*128*10=10240 = 10KB of text.
	// elevenKilobyte := strings.Repeat("logs-123", 8*128*11) // 8*128*11=11264 = 11KB of text.

	tests := []struct {
		expectedStatus Result
		expectError    bool
		input          string
		output         string
		exitCode       int
		execErr        error
	}{
		// Ok
		{Success, false, "OK", "OK", 0, nil},
		// Ok - truncated output
		// {probe.Success, false, elevenKilobyte, tenKilobyte, nil},
		// Run returns error
		{Unknown, true, "", "", 0, fmt.Errorf("test error")},
		// Unhealthy
		{Failure, false, "Fail", "", 1, nil},
	}
	for i, test := range tests {
		e := func(ctx context.Context, name string, args ...string) (int, []byte, error) {
			return test.exitCode, []byte(test.output), test.execErr
		}

		prober := execProber{excer: e}

		status, output, err := prober.Probe(context.Background(), "foo", "arg1", "arg2")
		if status != test.expectedStatus {
			t.Errorf("[%d] expected %v, got %v", i, test.expectedStatus, status)
		}
		if err != nil && test.expectError == false {
			t.Errorf("[%d] unexpected error: %v", i, err)
		}
		if err == nil && test.expectError == true {
			t.Errorf("[%d] unexpected non-error", i)
		}
		if test.output != output {
			t.Errorf("[%d] expected %s, got %s", i, test.output, output)
		}
	}
}

func TestRealExecer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("test not supported on Windows")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	script := `sleep 60 & echo $!`
	exitCode, out, err := realExecer(ctx, "sh", "-c", script)

	assert.NoError(t, err)
	assert.Equal(t, -1, exitCode)

	// OS can take a moment to process the SIGKILL to the process group
	time.Sleep(250 * time.Millisecond)

	output := strings.TrimSpace(string(out))
	if assert.NotEmpty(t, output) {
		childPid, err := strconv.Atoi(output)
		require.NoError(t, err, "Couldn't get child PID from stdout/stderr: %s", output)
		// os.FindProcess is a no-op on Unix-like systems and always succeeds; need to send signal 0 to probe it
		proc, _ := os.FindProcess(childPid)
		err = proc.Signal(syscall.Signal(0))
		if !assert.Equal(t, os.ErrProcessDone, err, "Child process was still running") {
			_ = proc.Kill()
		}
	}
}
