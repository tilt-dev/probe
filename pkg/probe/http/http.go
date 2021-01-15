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

package http

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/tilt-dev/probe/pkg/probe"
)

const (
	maxRespBodyLength = 10 * 1 << 10 // 10KB
)

// ErrLimitReached means that the read limit is reached.
var ErrLimitReached = errors.New("the read limit is reached")

func defaultTransport() *http.Transport {
	// TODO(milas): add http2 healthcheck -> https://github.com/kubernetes/apimachinery/blob/master/pkg/util/net/http.go#L173-L189
	return &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		DisableKeepAlives: true,
		Proxy:             http.ProxyURL(nil),
	}
}

// New creates Prober that will skip TLS verification while probing.
func New() Prober {
	return httpProber{defaultTransport()}
}

// Prober is an interface that defines the Probe function for doing HTTP readiness/liveness checks.
type Prober interface {
	Probe(url *url.URL, headers http.Header, timeout time.Duration) (probe.Result, string, error)
}

type httpProber struct {
	transport *http.Transport
}

// Probe returns a ProbeRunner capable of running an HTTP check.
func (pr httpProber) Probe(url *url.URL, headers http.Header, timeout time.Duration) (probe.Result, string, error) {
	pr.transport.DisableCompression = true // removes Accept-Encoding header
	client := &http.Client{
		Timeout:       timeout,
		Transport:     pr.transport,
		CheckRedirect: redirectChecker(),
	}
	return DoHTTPProbe(url, headers, client)
}

// GetHTTPInterface is an interface for making HTTP requests, that returns a response and error.
type GetHTTPInterface interface {
	Do(req *http.Request) (*http.Response, error)
}

// DoHTTPProbe checks if a GET request to the url succeeds.
// If the HTTP response code is successful (i.e. 400 > code >= 200), it returns Success.
// If the HTTP response code is unsuccessful or HTTP communication fails, it returns Failure.
// This is exported because some other packages may want to do direct HTTP probes.
func DoHTTPProbe(url *url.URL, headers http.Header, client GetHTTPInterface) (probe.Result, string, error) {
	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		// Convert errors into failures to catch timeouts.
		return probe.Failure, err.Error(), nil
	}
	if _, ok := headers["User-Agent"]; !ok {
		if headers == nil {
			headers = http.Header{}
		}
		// TODO(milas): add version support to package
		// explicitly set User-Agent so it's not set to default Go value
		headers.Set("User-Agent", fmt.Sprintf("tilt-probe/%s.%s", "0", "1"))
	}
	if _, ok := headers["Accept"]; !ok {
		// Accept header was not defined. accept all
		headers.Set("Accept", "*/*")
	} else if headers.Get("Accept") == "" {
		// Accept header was overridden but is empty. removing
		headers.Del("Accept")
	}
	req.Header = headers
	req.Host = headers.Get("Host")
	res, err := client.Do(req)
	if err != nil {
		// Convert errors into failures to catch timeouts.
		return probe.Failure, err.Error(), nil
	}
	defer res.Body.Close()
	b, err := readAtMost(res.Body, maxRespBodyLength)
	if err != nil && err != ErrLimitReached {
		return probe.Failure, "", err
	}
	body := string(b)
	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusBadRequest {
		if res.StatusCode >= http.StatusMultipleChoices { // Redirect
			// klog.V(4).Infof("Probe terminated redirects for %s, Response: %v", url.String(), *res)
			return probe.Warning, body, nil
		}
		// klog.V(4).Infof("Probe succeeded for %s, Response: %v", url.String(), *res)
		return probe.Success, body, nil
	}
	// klog.V(4).Infof("Probe failed for %s with request headers %v, response body: %v", url.String(), headers, body)
	return probe.Failure, fmt.Sprintf("HTTP probe failed with statuscode: %d", res.StatusCode), nil
}

func redirectChecker() func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if req.URL.Hostname() != via[0].URL.Hostname() {
			return http.ErrUseLastResponse
		}
		// Default behavior: stop after 10 redirects.
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
}

// ReadAtMost reads up to `limit` bytes from `r`, and reports an error
// when `limit` bytes are read.
func readAtMost(r io.Reader, limit int64) ([]byte, error) {
	limitedReader := &io.LimitedReader{R: r, N: limit}
	data, err := ioutil.ReadAll(limitedReader)
	if err != nil {
		return data, err
	}
	if limitedReader.N <= 0 {
		return data, ErrLimitReached
	}
	return data, nil
}
