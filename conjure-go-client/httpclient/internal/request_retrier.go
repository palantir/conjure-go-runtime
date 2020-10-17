package internal

import (
	"context"
	"math/rand"
	"net/http"
	"net/url"
	"strings"

	"github.com/palantir/pkg/retry"
	werror "github.com/palantir/witchcraft-go-error"
)

const (
	meshSchemePrefix = "mesh-"
)

type RequestRetrier struct {
	currentURI string
	retrier    retry.Retrier
	uris       []string
	offset     int
	failedURIs map[string]struct{}
	maxRetries int
	retryCount int
}

func NewRequestRetrier(uris []string, retrier retry.Retrier, maxRetries int) *RequestRetrier {
	offset := rand.Intn(len(uris))
	return &RequestRetrier{
		currentURI: uris[offset],
		retrier:    retrier,
		uris:       uris,
		offset:     offset,
		failedURIs: map[string]struct{}{},
		maxRetries: maxRetries,
		retryCount: 0,
	}
}

func (r *RequestRetrier) ShouldGetNextURI(resp *http.Response, respErr error) bool {
	if r.retryCount == 0 {
		return true
	}
	return r.retryCount <= r.maxRetries &&
		!r.isMeshURI(r.currentURI) &&
		r.responseAndErrRetriable(resp, respErr)
}

func (r *RequestRetrier) GetNextURI(ctx context.Context, resp *http.Response, respErr error) (string, error) {
	defer func() {
		r.retryCount++
	}()
	if r.retryCount == 0 {
		return r.removeMeshSchemeIfPresent(r.currentURI), nil
	} else if !r.ShouldGetNextURI(resp, respErr) {
		return "", r.errorFromRespError(ctx, resp, respErr)
	}
	return r.doRetrySelection(resp, respErr), nil
}

func (r *RequestRetrier) doRetrySelection(resp *http.Response, respErr error) string {
	retryFn := r.getRetryFn(resp, respErr)
	if retryFn != nil {
		retryFn()
		return r.currentURI
	}
	return ""
}

func (r *RequestRetrier) responseAndErrRetriable(resp *http.Response, respErr error) bool {
	return r.getRetryFn(resp, respErr) != nil
}

func (r *RequestRetrier) getRetryFn(resp *http.Response, respErr error) func() {
	if retryOther, _ := isThrottleResponse(resp, respErr); retryOther {
		// 429: throttle
		// Immediately backoff and select the next URI.
		// TODO(whickman): use the retry-after header once #81 is resolved
		return r.nextURIAndBackoff
	} else if isUnavailableResponse(resp, respErr) || resp == nil {
		// 503: go to next node
		// Or if we get a nil response, we can assume there is a problem with host and can move on to the next.
		return r.nextURIOrBackoff
	} else if shouldTryOther, otherURI := isRetryOtherResponse(resp); shouldTryOther {
		// 308: go to next node, or particular node if provided.
		if otherURI != nil {
			return func() {
				r.setURIAndResetBackoff(otherURI)
			}
		}
		return r.nextURIOrBackoff
	}
	return nil
}

func (r *RequestRetrier) setURIAndResetBackoff(otherURI *url.URL) {
	nextURI := otherURI.String()
	r.retrier.Reset()
	r.currentURI = nextURI
}

// If lastURI was already marked failed, we perform a backoff as determined by the retrier before returning the next URI and its offset.
// Otherwise, we add lastURI to failedURIs and return the next URI and its offset immediately.
func (r *RequestRetrier) nextURIOrBackoff() {
	_, performBackoff := r.failedURIs[r.currentURI]
	r.markFailedAndMoveToNextURI()
	// If the URI has failed before, perform a backoff
	if performBackoff || len(r.uris) == 1 {
		r.retrier.Next()
	}
}

// Marks the current URI as failed, gets the next URI, and performs a backoff as determined by the retrier.
func (r *RequestRetrier) nextURIAndBackoff() {
	r.markFailedAndMoveToNextURI()
	r.retrier.Next()
}

func (r *RequestRetrier) markFailedAndMoveToNextURI() {
	r.failedURIs[r.currentURI] = struct{}{}
	nextURIOffset := (r.offset + 1) % len(r.uris)
	nextURI := r.uris[nextURIOffset]
	r.currentURI = nextURI
	r.offset = nextURIOffset
}

func (r *RequestRetrier) removeMeshSchemeIfPresent(uri string) string {
	if r.isMeshURI(uri) {
		return strings.Replace(uri, meshSchemePrefix, "", 1)
	}
	return uri
}

func (r *RequestRetrier) isMeshURI(uri string) bool {
	return strings.HasPrefix(uri, meshSchemePrefix)
}

func (r *RequestRetrier) errorFromRespError(ctx context.Context, resp *http.Response, respErr error) error {
	message := "GetNextURI called, but retry should not be attempted"
	params := []werror.Param{
		werror.SafeParam("retryCount", r.retryCount),
		werror.SafeParam("maxRetries", r.maxRetries),
		werror.SafeParam("statusCodeRetriable", r.responseAndErrRetriable(resp, respErr)),
		werror.SafeParam("uriInMesh", r.isMeshURI(r.currentURI)),
	}
	if respErr != nil {
		return werror.WrapWithContextParams(ctx, respErr, message, params...)
	}
	return werror.ErrorWithContextParams(ctx, message, params...)
}
