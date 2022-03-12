package httpclient

import (
	"net/http"
	urlpkg "net/url"
	"strings"
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
	err := u.setRequestURLAndHost(req)
	if err != nil {
		return nil, err
	}
	resp, err := next.RoundTrip(req)
	u.offset = (u.offset + 1) % len(u.uris)
	if _, url := isRedirectResponse(resp); url != nil {
		if !url.IsAbs() {
			url = req.URL.ResolveReference(url)
		}
		u.redirectURL = url
	} else {
		u.offset = (u.offset + 1) % len(u.uris)
	}
	return resp, err
}

func (u *uriMiddleware) setRequestURLAndHost(req *http.Request) error {
	var url *urlpkg.URL
	if u.redirectURL != nil {
		url = u.redirectURL
		u.redirectURL = nil
	} else {
		uri := u.uris[u.offset]
		if isMeshURI(uri) {
			// Mesh URIs should not be retried
			u.cancelFunc()
			uri = strings.Replace(uri, meshSchemePrefix, "", 1)
		}
		parsedUrl, err := urlpkg.Parse(uri)
		if err != nil {
			return err
		}
		removeEmptyPort(parsedUrl)
		url = parsedUrl.ResolveReference(req.URL)
	}
	req.URL = url
	req.Host = url.Host
	return nil
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

func isRedirectResponse(resp *http.Response) (bool, *urlpkg.URL) {
	if resp == nil || !isRetryOtherStatusCode(resp.StatusCode) {
		return false, nil
	}
	locationStr := resp.Header.Get("Location")
	return true, parseLocationURL(locationStr)
}

func isRetryOtherStatusCode(statusCode int) bool {
	return statusCode == 307 || statusCode == 308
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
