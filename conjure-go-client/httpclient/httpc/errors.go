package httpc

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/codecs"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/errors"
	werror "github.com/palantir/witchcraft-go-error"
)

type ErrEmptyURIs struct{}

func (ErrEmptyURIs) Error() string {
	return "httpclient URLs must not be empty"
}

// unwrapURLError converts a *url.Error to a werror, preserving any werror
// params on the underlying error.
func unwrapURLError(ctx context.Context, respErr error) error {
	if respErr == nil {
		return nil
	}
	urlErr, ok := respErr.(*url.Error)
	if !ok {
		return respErr
	}
	params := []werror.Param{werror.SafeParam("requestMethod", urlErr.Op)}
	if parsedURL, _ := url.Parse(urlErr.URL); parsedURL != nil {
		params = append(params,
			werror.SafeParam("requestHost", parsedURL.Host),
			werror.UnsafeParam("requestPath", parsedURL.Path))
	}
	return werror.WrapWithContextParams(ctx, urlErr.Err, "httpclient request failed", params...)
}

// defaultRestErrorDecoder handles responses with status code >= 307.
// For JSON responses, it attempts to unmarshal the body as a Conjure error.
// For non-JSON responses or failed unmarshal, it includes the raw body as an
// unsafe parameter. For 3xx responses, it extracts the Location header.
//
// Use StatusCodeFromError(err) to retrieve the code from the error,
// and DisableRestErrors() to disable this decoder on your client.
type defaultRestErrorDecoder struct {
	conjureErrorDecoder errors.ConjureErrorDecoder
}

func (defaultRestErrorDecoder) Handles(resp *http.Response) bool {
	return resp.StatusCode >= http.StatusTemporaryRedirect
}

func (d defaultRestErrorDecoder) DecodeError(resp *http.Response) error {
	safeParams := map[string]interface{}{
		"statusCode": resp.StatusCode,
	}
	unsafeParams := map[string]interface{}{}
	if resp.StatusCode >= http.StatusTemporaryRedirect &&
		resp.StatusCode < http.StatusBadRequest {
		location, err := resp.Location()
		if err == nil {
			unsafeParams["location"] = location.String()
		}
	}
	wSafeParams := werror.SafeParams(safeParams)
	wUnsafeParams := werror.UnsafeParams(unsafeParams)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return werror.Wrap(err, "server returned an error and failed to read body", wSafeParams, wUnsafeParams)
	}
	if len(body) == 0 {
		return werror.Error(resp.Status, wSafeParams, wUnsafeParams)
	}

	// If JSON, try to unmarshal as Conjure error.
	if isJSON := strings.Contains(resp.Header.Get("Content-Type"), codecs.JSON.ContentType()); !isJSON {
		return werror.Error(resp.Status, wSafeParams, wUnsafeParams, werror.UnsafeParam("responseBody", string(body)))
	}
	var conjureErr errors.Error
	var jsonErr error
	if d.conjureErrorDecoder != nil {
		conjureErr, jsonErr = errors.UnmarshalErrorWithDecoder(d.conjureErrorDecoder, body)
	} else {
		conjureErr, jsonErr = errors.UnmarshalError(body)
	}
	if jsonErr != nil {
		return werror.Error(resp.Status, wSafeParams, wUnsafeParams, werror.UnsafeParam("responseBody", string(body)))
	}
	return werror.Wrap(conjureErr, "", wSafeParams, wUnsafeParams)
}
