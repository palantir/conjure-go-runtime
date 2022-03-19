// Copyright (c) 2022 Palantir Technologies. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package httpclient

import (
	"net/http"
	urlpkg "net/url"
	"strings"

	werror "github.com/palantir/witchcraft-go-error"
)

const (
	meshSchemePrefix = "mesh-"
)

type uriMiddleware struct {
	uris        []string
	offset      int
	redirectURL *urlpkg.URL
	cancelFunc  func()
}

func NewURIMiddleware(uris []string, cancelFunc func()) Middleware {
	offset := 0
	return &uriMiddleware{
		uris:        uris,
		offset:      offset,
		redirectURL: nil,
		cancelFunc:  cancelFunc,
	}
}

func (u *uriMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	url, err := u.getURL(req)
	if err != nil {
		return nil, err
	}
	req.URL = url
	req.Host = url.Host
	resp, err := next.RoundTrip(req)
	if _, redirectURL := isRedirectError(err); redirectURL != nil {
		if !redirectURL.IsAbs() {
			redirectURL = req.URL.ResolveReference(redirectURL)
		}
		u.redirectURL = redirectURL
	}
	if err != nil {
		params := []werror.Param{
			werror.SafeParam("requestMethod", req.Method),
			werror.SafeParam("requestHost", url.Host),
			werror.UnsafeParam("requestPath", url.Path),
		}
		return nil, werror.Wrap(err, "httpclient request failed", params...)
	}
	return resp, err
}

func (u *uriMiddleware) getURL(req *http.Request) (*urlpkg.URL, error) {
	if u.redirectURL != nil {
		defer func() {
			u.redirectURL = nil
		}()
		return u.redirectURL, nil
	}
	uri := u.uris[u.offset]
	u.offset = (u.offset + 1) % len(u.uris)
	if isMeshURI(uri) {
		// Mesh URIs should not be retried
		u.cancelFunc()
		uri = strings.Replace(uri, meshSchemePrefix, "", 1)
	}
	parsedURL, err := urlpkg.Parse(uri)
	if err != nil {
		return nil, err
	}
	removeEmptyPort(parsedURL)
	return parsedURL.ResolveReference(req.URL), nil
}

// removeEmptyPort() strips the empty port in ":port" to ""
// as mandated by RFC 3986 Section 6.2.3.
func removeEmptyPort(url *urlpkg.URL) {
	if url.Port() == "" {
		url.Host = strings.TrimSuffix(url.Host, ":")
	}
}

func isMeshURI(uri string) bool {
	return strings.HasPrefix(uri, meshSchemePrefix)
}

func isRedirectError(err error) (bool, *urlpkg.URL) {
	errCode, _ := StatusCodeFromError(err)
	if !isRedirectStatusCode(errCode) {
		return false, nil
	}
	_, unsafeParams := werror.ParamsFromError(err)
	locationStr, ok := unsafeParams["location"].(string)
	if !ok {
		return true, nil
	}
	return true, parseLocationURL(locationStr)
}

func isRedirectStatusCode(statusCode int) bool {
	return statusCode == http.StatusTemporaryRedirect || statusCode == http.StatusPermanentRedirect
}

func parseLocationURL(locationStr string) *urlpkg.URL {
	if locationStr == "" {
		return nil
	}
	locationURL, err := urlpkg.Parse(locationStr)
	if err != nil {
		// Unable to parse location as something we recognize
		return nil
	}
	return locationURL
}
