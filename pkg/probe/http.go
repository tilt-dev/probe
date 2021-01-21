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
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"

	"k8s.io/klog/v2"
)

const (
	maxRespBodyLength = 10 * 1 << 10 // 10KB
)

// ErrLimitReached means that the read limit is reached.
var ErrLimitReached = errors.New("the read limit is reached")

func defaultTransport() *http.Transport {
	// TODO(milas): add http2 healthcheck -> https://github.com/kubernetes/apimachinery/blob/master/pkg/util/net/http.go#L173-L189
	return &http.Transport{
		TLSClientConfig:    &tls.Config{InsecureSkipVerify: true},
		DisableCompression: true, // removes Accept-Encoding header
		DisableKeepAlives:  true,
		Proxy:              http.ProxyURL(nil),
	}
}

// NewHTTPGet creates a probe that will perform an HTTP GET request.
func NewHTTPGet(url *url.URL, headers http.Header) HTTPGet {
	return HTTPGet{
		client: &http.Client{
			Transport:     defaultTransport(),
			CheckRedirect: redirectChecker(),
		},
		url:     url,
		headers: headers,
	}
}

type HTTPGet struct {
	client  *http.Client
	url     *url.URL
	headers http.Header
}

var _ Probe = &HTTPGet{}

// URL returns a copy of the URL used by the probe.
func (h HTTPGet) URL() url.URL {
	// force a copy so callers can't mutate the URL
	return *h.url
}

// Headers returns a copy of the headers used by the probe.
func (h HTTPGet) Headers() http.Header {
	out := make(http.Header)
	for key, values := range h.headers {
		out[key] = values
	}
	return out
}

// Execute checks if a GET request to the URL succeeds.
// If the HTTPGet response code is successful (i.e. 400 > code >= 200), it returns Success.
// If the HTTPGet response code is unsuccessful or HTTPGet communication fails, it returns Failure.
func (h HTTPGet) Execute(ctx context.Context) (Result, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url.String(), nil)
	if err != nil {
		// Convert errors into failures to catch timeouts.
		return Failure, err.Error(), nil
	}
	if _, ok := h.headers["User-Agent"]; !ok {
		if h.headers == nil {
			h.headers = http.Header{}
		}
		// TODO(milas): add version support to package
		// explicitly set User-Agent so it's not set to default Go value
		h.headers.Set("User-Agent", fmt.Sprintf("tilt-probe/%s.%s", "0", "1"))
	}
	if _, ok := h.headers["Accept"]; !ok {
		// Accept header was not defined. accept all
		h.headers.Set("Accept", "*/*")
	} else if h.headers.Get("Accept") == "" {
		// Accept header was overridden but is empty. removing
		h.headers.Del("Accept")
	}
	req.Header = h.headers
	req.Host = h.headers.Get("Host")
	res, err := h.client.Do(req)
	if err != nil {
		// Convert errors into failures to catch timeouts.
		return Failure, err.Error(), nil
	}
	defer res.Body.Close()
	b, err := readAtMost(res.Body, maxRespBodyLength)
	if err != nil {
		if err == ErrLimitReached {
			klog.V(4).Infof("Non fatal body truncation for %s, Response: %v", h.url.String(), *res)
		} else {
			return Failure, "", err
		}
	}
	body := string(b)
	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusBadRequest {
		if res.StatusCode >= http.StatusMultipleChoices { // Redirect
			klog.V(4).Infof("Execute terminated redirects for %s, Response: %v", h.url.String(), *res)
			return Warning, body, nil
		}
		klog.V(4).Infof("Execute succeeded for %s, Response: %v", h.url.String(), *res)
		return Success, body, nil
	}
	klog.V(4).Infof("Execute failed for %s with request headers %v, response body: %v", h.url.String(), h.headers, body)
	return Failure, fmt.Sprintf("HTTPGet probe failed with statuscode: %d", res.StatusCode), nil
}

// GetHTTPInterface is an interface for making HTTPGet requests, that returns a response and error.
type GetHTTPInterface interface {
	Do(req *http.Request) (*http.Response, error)
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

// readAtMost reads up to `limit` bytes from `r`, and reports an error
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
