package prober

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errMock = errors.New("mock error")

func TestNewManager(t *testing.T) {
	m := NewManager()
	assert.NotNil(t, m.execProber)
	assert.NotNil(t, m.httpProber)
	assert.NotNil(t, m.tcpProber)
}

type mockExecProber struct {
	mock.Mock
}

func (m *mockExecProber) Probe(ctx context.Context, name string, args ...string) (Result, string, error) {
	callArgs := m.Called(ctx, name, args)
	return callArgs.Get(0).(Result), callArgs.String(1), callArgs.Error(2)
}

func TestManager_Exec(t *testing.T) {
	execProber := &mockExecProber{}
	execProber.On("Probe", mock.Anything, "foo", []string{"arg1", "arg2"}).Return(Success, "mock", errMock)

	m := Manager{execProber: execProber}
	probeFunc := m.Exec("foo", "arg1", "arg2")

	result, out, err := probeFunc(context.Background())
	assert.Equal(t, Success, result)
	assert.Equal(t, "mock", out)
	assert.ErrorIs(t, err, errMock)
	execProber.AssertExpectations(t)
}

type mockHTTPGetProber struct {
	mock.Mock
}

func (m *mockHTTPGetProber) Probe(ctx context.Context, url *url.URL, headers http.Header) (Result, string, error) {
	callArgs := m.Called(ctx, url, headers)
	return callArgs.Get(0).(Result), callArgs.String(1), callArgs.Error(2)
}

func TestManager_HTTPGet(t *testing.T) {
	httpProber := &mockHTTPGetProber{}
	urlCmp := func(u *url.URL) bool {
		return u.String() == "http://tilt.dev/status"
	}
	fakeHeaders := func() http.Header {
		return http.Header{
			"X-Fake-Header": []string{"value1", "value2"},
			"Content-Type":  []string{"application/json"},
		}
	}
	httpProber.On("Probe", mock.Anything, mock.MatchedBy(urlCmp), fakeHeaders()).Return(Success, "mock", errMock)

	m := Manager{httpProber: httpProber}
	u, err := url.ParseRequestURI("http://tilt.dev/status")
	require.NoError(t, err)
	probeFunc := m.HTTPGet(u, fakeHeaders())

	result, out, err := probeFunc(context.Background())
	assert.Equal(t, Success, result)
	assert.Equal(t, "mock", out)
	assert.ErrorIs(t, err, errMock)
	httpProber.AssertExpectations(t)
}

type mockTCPSocketProber struct {
	mock.Mock
}

func (m *mockTCPSocketProber) Probe(ctx context.Context, host string, port int) (Result, string, error) {
	callArgs := m.Called(ctx, host, port)
	return callArgs.Get(0).(Result), callArgs.String(1), callArgs.Error(2)
}

func TestManager_TCPSocket(t *testing.T) {
	tcpProber := &mockTCPSocketProber{}
	tcpProber.On("Probe", mock.Anything, "localhost", 1234).Return(Success, "mock", errMock)

	m := Manager{tcpProber: tcpProber}
	probeFunc := m.TCPSocket("localhost", 1234)

	result, out, err := probeFunc(context.Background())
	assert.Equal(t, Success, result)
	assert.Equal(t, "mock", out)
	assert.ErrorIs(t, err, errMock)
	tcpProber.AssertExpectations(t)
}
