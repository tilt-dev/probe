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
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const FailureCode int = -1

const testTimeout = time.Second * 30

func setEnv(key, value string) func() {
	originalValue := os.Getenv(key)
	os.Setenv(key, value)
	if len(originalValue) > 0 {
		return func() {
			os.Setenv(key, originalValue)
		}
	}
	return func() {}
}

func unsetEnv(key string) func() {
	originalValue := os.Getenv(key)
	os.Unsetenv(key)
	if len(originalValue) > 0 {
		return func() {
			os.Setenv(key, originalValue)
		}
	}
	return func() {}
}

func TestHTTPProbeProxy(t *testing.T) {
	res := "welcome to http probe proxy"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, res)
	}))
	defer server.Close()

	localProxy := server.URL

	defer setEnv("http_proxy", localProxy)()
	defer setEnv("HTTP_PROXY", localProxy)()
	defer unsetEnv("no_proxy")()
	defer unsetEnv("NO_PROXY")()

	prober := NewHTTPGetProber()

	// take some time to wait server boot
	time.Sleep(2 * time.Second)
	url, err := url.Parse("http://example.com")
	if err != nil {
		t.Errorf("proxy test unexpected error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, response, _ := prober.Probe(ctx, url, http.Header{})

	if response == res {
		t.Errorf("proxy test unexpected error: the probe is using proxy")
	}
}

func TestHTTPProbeChecker(t *testing.T) {
	handleReq := func(s int, body string) func(w http.ResponseWriter, r *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(s)
			w.Write([]byte(body))
		}
	}

	// Echo handler that returns the contents of request headers in the body
	headerEchoHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		output := ""
		for k, arr := range r.Header {
			for _, v := range arr {
				output += fmt.Sprintf("%s: %s\n", k, v)
			}
		}
		w.Write([]byte(output))
	}

	// Handler that returns the number of request headers in the body
	headerCounterHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(strconv.Itoa(len(r.Header))))
	}

	// Handler that returns the keys of request headers in the body
	headerKeysNamesHandler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		keys := make([]string, 0, len(r.Header))
		for k := range r.Header {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		w.Write([]byte(strings.Join(keys, "\n")))
	}

	redirectHandler := func(s int, bad bool) func(w http.ResponseWriter, r *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.Redirect(w, r, "/new", s)
			} else if bad && r.URL.Path == "/new" {
				http.Error(w, "", http.StatusInternalServerError)
			}
		}
	}

	prober := NewHTTPGetProber()
	testCases := []struct {
		handler    func(w http.ResponseWriter, r *http.Request)
		reqHeaders http.Header
		health     Result
		accBody    string
		notBody    string
	}{
		// The probe will be filled in below.  This is primarily testing that an HTTP GET happens.
		{
			handler: handleReq(http.StatusOK, "ok body"),
			health:  Success,
			accBody: "ok body",
		},
		{
			handler:    headerCounterHandler,
			reqHeaders: http.Header{},
			health:     Success,
			accBody:    "3",
		},
		{
			handler:    headerKeysNamesHandler,
			reqHeaders: http.Header{},
			health:     Success,
			accBody:    "Accept\nConnection\nUser-Agent",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"Accept-Encoding": {"gzip"},
			},
			health:  Success,
			accBody: "Accept-Encoding: gzip",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"Accept-Encoding": {"foo"},
			},
			health:  Success,
			accBody: "Accept-Encoding: foo",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"Accept-Encoding": {""},
			},
			health:  Success,
			accBody: "Accept-Encoding: \n",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"X-Muffins-Or-Cupcakes": {"muffins"},
			},
			health:  Success,
			accBody: "X-Muffins-Or-Cupcakes: muffins",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"User-Agent": {"foo/1.0"},
			},
			health:  Success,
			accBody: "User-Agent: foo/1.0",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"User-Agent": {""},
			},
			health:  Success,
			notBody: "User-Agent",
		},
		{
			handler:    headerEchoHandler,
			reqHeaders: http.Header{},
			health:     Success,
			accBody:    "User-Agent: tilt-probe/",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"User-Agent": {"foo/1.0"},
				"Accept":     {"text/html"},
			},
			health:  Success,
			accBody: "Accept: text/html",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"User-Agent": {"foo/1.0"},
				"Accept":     {"foo/*"},
			},
			health:  Success,
			accBody: "User-Agent: foo/1.0",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"X-Muffins-Or-Cupcakes": {"muffins"},
				"Accept":                {"foo/*"},
			},
			health:  Success,
			accBody: "X-Muffins-Or-Cupcakes: muffins",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"Accept": {"foo/*"},
			},
			health:  Success,
			accBody: "Accept: foo/*",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"Accept": {""},
			},
			health:  Success,
			notBody: "Accept:",
		},
		{
			handler: headerEchoHandler,
			reqHeaders: http.Header{
				"User-Agent": {"foo/1.0"},
				"Accept":     {""},
			},
			health:  Success,
			notBody: "Accept:",
		},
		{
			handler:    headerEchoHandler,
			reqHeaders: http.Header{},
			health:     Success,
			accBody:    "Accept: */*",
		},
		{
			// Echo handler that returns the contents of Host in the body
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.Write([]byte(r.Host))
			},
			reqHeaders: http.Header{
				"Host": {"muffins.cupcakes.org"},
			},
			health:  Success,
			accBody: "muffins.cupcakes.org",
		},
		{
			handler: handleReq(FailureCode, "fail body"),
			health:  Failure,
		},
		{
			handler: handleReq(http.StatusInternalServerError, "fail body"),
			health:  Failure,
		},
		{
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(3 * time.Second)
			},
			health: Failure,
		},
		{
			handler: redirectHandler(http.StatusMovedPermanently, false), // 301
			health:  Success,
		},
		{
			handler: redirectHandler(http.StatusMovedPermanently, true), // 301
			health:  Failure,
		},
		{
			handler: redirectHandler(http.StatusFound, false), // 302
			health:  Success,
		},
		{
			handler: redirectHandler(http.StatusFound, true), // 302
			health:  Failure,
		},
		{
			handler: redirectHandler(http.StatusTemporaryRedirect, false), // 307
			health:  Success,
		},
		{
			handler: redirectHandler(http.StatusTemporaryRedirect, true), // 307
			health:  Failure,
		},
		{
			handler: redirectHandler(http.StatusPermanentRedirect, false), // 308
			health:  Success,
		},
		{
			handler: redirectHandler(http.StatusPermanentRedirect, true), // 308
			health:  Failure,
		},
	}
	for i, test := range testCases {
		t.Run(fmt.Sprintf("case-%2d", i), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				test.handler(w, r)
			}))
			defer server.Close()
			u, err := url.Parse(server.URL)
			if err != nil {
				t.Errorf("case %d: unexpected error: %v", i, err)
			}
			_, port, err := net.SplitHostPort(u.Host)
			if err != nil {
				t.Errorf("case %d: unexpected error: %v", i, err)
			}
			_, err = strconv.Atoi(port)
			if err != nil {
				t.Errorf("case %d: unexpected error: %v", i, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			health, output, err := prober.Probe(ctx, u, test.reqHeaders)
			if test.health == Unknown && err == nil {
				t.Errorf("case %d: expected error", i)
			}
			if test.health != Unknown && err != nil {
				t.Errorf("case %d: unexpected error: %v", i, err)
			}
			if health != test.health {
				t.Errorf("case %d: expected %v, got %v", i, test.health, health)
			}
			if health != Failure && test.health != Failure {
				if !strings.Contains(output, test.accBody) {
					t.Errorf("Expected response body to contain %v, got %v", test.accBody, output)
				}
				if test.notBody != "" && strings.Contains(output, test.notBody) {
					t.Errorf("Expected response not to contain %v, got %v", test.notBody, output)
				}
			}
		})
	}
}

func TestHTTPProbeChecker_NonLocalRedirects(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			loc, _ := url.QueryUnescape(r.URL.Query().Get("loc"))
			http.Redirect(w, r, loc, http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/success":
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "", http.StatusInternalServerError)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	newportServer := httptest.NewServer(handler)
	defer newportServer.Close()

	testCases := map[string]struct {
		redirect             string
		expectLocalResult    Result
		expectNonLocalResult Result
	}{
		"local success":   {"/success", Success, Success},
		"local fail":      {"/fail", Failure, Failure},
		"newport success": {newportServer.URL + "/success", Success, Success},
		"newport fail":    {newportServer.URL + "/fail", Failure, Failure},
		"bogus nonlocal":  {"http://0.0.0.0/fail", Warning, Failure},
		"redirect loop":   {"/loop", Failure, Failure},
	}
	for desc, test := range testCases {
		t.Run(desc, func(t *testing.T) {
			prober := NewHTTPGetProber()
			target, err := url.Parse(server.URL + "/redirect?loc=" + url.QueryEscape(test.redirect))
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()
			result, _, _ := prober.Probe(ctx, target, nil)
			assert.Equal(t, test.expectLocalResult, result)
		})
	}
}

func TestHTTPProbeChecker_HostHeaderPreservedAfterRedirect(t *testing.T) {
	successHostHeader := "www.success.com"
	failHostHeader := "www.fail.com"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/success", http.StatusFound)
		case "/success":
			if r.Host == successHostHeader {
				w.WriteHeader(http.StatusOK)
			} else {
				http.Error(w, "", http.StatusBadRequest)
			}
		default:
			http.Error(w, "", http.StatusInternalServerError)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	testCases := map[string]struct {
		hostHeader     string
		expectedResult Result
	}{
		"success": {successHostHeader, Success},
		"fail":    {failHostHeader, Failure},
	}
	for desc, test := range testCases {
		headers := http.Header{}
		headers.Add("Host", test.hostHeader)
		t.Run(desc, func(t *testing.T) {
			prober := NewHTTPGetProber()
			target, err := url.Parse(server.URL + "/redirect")
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()
			result, _, _ := prober.Probe(ctx, target, headers)
			assert.Equal(t, test.expectedResult, result)
		})
	}
}

func TestHTTPProbeChecker_PayloadTruncated(t *testing.T) {
	successHostHeader := "www.success.com"
	oversizePayload := bytes.Repeat([]byte("a"), maxRespBodyLength+1)
	truncatedPayload := bytes.Repeat([]byte("a"), maxRespBodyLength)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/success":
			if r.Host == successHostHeader {
				w.WriteHeader(http.StatusOK)
				w.Write(oversizePayload)
			} else {
				http.Error(w, "", http.StatusBadRequest)
			}
		default:
			http.Error(w, "", http.StatusInternalServerError)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	headers := http.Header{}
	headers.Add("Host", successHostHeader)
	t.Run("truncated payload", func(t *testing.T) {
		prober := NewHTTPGetProber()
		target, err := url.Parse(server.URL + "/success")
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		result, body, err := prober.Probe(ctx, target, headers)
		assert.NoError(t, err)
		assert.Equal(t, result, Success)
		assert.Equal(t, body, string(truncatedPayload))
	})
}

func TestHTTPProbeChecker_PayloadNormal(t *testing.T) {
	successHostHeader := "www.success.com"
	normalPayload := bytes.Repeat([]byte("a"), maxRespBodyLength-1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/success":
			if r.Host == successHostHeader {
				w.WriteHeader(http.StatusOK)
				w.Write(normalPayload)
			} else {
				http.Error(w, "", http.StatusBadRequest)
			}
		default:
			http.Error(w, "", http.StatusInternalServerError)
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	headers := http.Header{}
	headers.Add("Host", successHostHeader)
	t.Run("normal payload", func(t *testing.T) {
		prober := NewHTTPGetProber()
		target, err := url.Parse(server.URL + "/success")
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		result, body, err := prober.Probe(ctx, target, headers)
		assert.NoError(t, err)
		assert.Equal(t, result, Success)
		assert.Equal(t, body, string(normalPayload))
	})
}
