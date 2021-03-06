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
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/utils/exec"
)

type FakeCmd struct {
	out    []byte
	stdout []byte
	err    error
	writer io.Writer
}

func (f *FakeCmd) Run() error {
	return nil
}

func (f *FakeCmd) CombinedOutput() ([]byte, error) {
	return f.out, f.err
}

func (f *FakeCmd) Output() ([]byte, error) {
	return f.stdout, f.err
}

func (f *FakeCmd) SetDir(dir string) {}

func (f *FakeCmd) SetStdin(in io.Reader) {}

func (f *FakeCmd) SetStdout(out io.Writer) {
	f.writer = out
}

func (f *FakeCmd) SetStderr(out io.Writer) {
	f.writer = out
}

func (f *FakeCmd) SetEnv(env []string) {}

func (f *FakeCmd) Stop() {}

func (f *FakeCmd) Start() error {
	if f.writer != nil {
		f.writer.Write(f.out)
		return f.err
	}
	return f.err
}

func (f *FakeCmd) Wait() error { return nil }

func (f *FakeCmd) StdoutPipe() (io.ReadCloser, error) {
	return nil, nil
}

func (f *FakeCmd) StderrPipe() (io.ReadCloser, error) {
	return nil, nil
}

type fakeExitError struct {
	exited     bool
	statusCode int
}

func (f *fakeExitError) String() string {
	return f.Error()
}

func (f *fakeExitError) Error() string {
	return "fake exit"
}

func (f *fakeExitError) Exited() bool {
	return f.exited
}

func (f *fakeExitError) ExitStatus() int {
	return f.statusCode
}

var _ exec.ExitError = &fakeExitError{}

type fakeExecutor struct {
	mock.Mock
}

func (f *fakeExecutor) Command(cmd string, args ...string) exec.Cmd {
	callArgs := f.Called(cmd, args)
	return callArgs.Get(0).(exec.Cmd)
}

func (f *fakeExecutor) CommandContext(ctx context.Context, cmd string, args ...string) exec.Cmd {
	callArgs := f.Called(ctx, cmd, args)
	return callArgs.Get(0).(exec.Cmd)
}

func (f *fakeExecutor) LookPath(file string) (string, error) {
	return file, nil
}

func TestNewExecProber(t *testing.T) {
	prober := NewExecProber()
	execProber := prober.(execProber)
	assert.Equal(t, osExecRunner, execProber.runner)
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
		err            error
	}{
		// Ok
		{Success, false, "OK", "OK", nil},
		// Ok
		{Success, false, "OK", "OK", &fakeExitError{true, 0}},
		// Ok - truncated output
		// {probe.Success, false, elevenKilobyte, tenKilobyte, nil},
		// Run returns error
		{Unknown, true, "", "", fmt.Errorf("test error")},
		// Unhealthy
		{Failure, false, "Fail", "", &fakeExitError{true, 1}},
	}
	for i, test := range tests {
		fake := FakeCmd{
			out: []byte(test.output),
			err: test.err,
		}

		mockExecutor := &fakeExecutor{}
		// there's no clean way to assert on the created command other than a mock
		mockExecutor.On("CommandContext", mock.Anything, "foo", []string{"arg1", "arg2"}).Return(&fake)

		prober := execProber{runner: mockExecutor}

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

		mockExecutor.AssertExpectations(t)
	}
}
