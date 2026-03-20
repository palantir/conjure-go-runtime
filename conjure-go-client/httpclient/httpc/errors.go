package httpc

import (
	"context"
	"fmt"
	"net/url"

	werror "github.com/palantir/witchcraft-go-error"
)

var errEmptyURIs = fmt.Errorf("httpclient URLs must not be empty")

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
