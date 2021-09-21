package internal

import (
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"sync/atomic"
	"time"
)

const (
	failureWeight = 10.0
	failureMemory = 30 * time.Second
)

type BalancedURIScoringMiddleware interface {
	GetURIsInOrderOfIncreasingScore() []string
	RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error)
}

var _ BalancedURIScoringMiddleware = (*balancedScorer)(nil)

type balancedScorer struct {
	uriInfos map[string]uriInfo
}

type uriInfo struct {
	inflight       int32
	recentFailures CourseExponentialDecayReservoir
}

func NewBalancedURIScoringMiddleware(uris []string, nanoClock func() int64) BalancedURIScoringMiddleware {
	uriInfos := make(map[string]uriInfo, len(uris))
	for _, uri := range uris {
		uriInfos[uri] = uriInfo{
			recentFailures: NewCourseExponentialDecayReservoir(nanoClock, failureMemory),
		}
	}
	return &balancedScorer{
		uriInfos,
	}
}

func (u *balancedScorer) GetURIsInOrderOfIncreasingScore() []string {
	uris := make([]string, len(u.uriInfos))
	scores := make(map[string]int32, len(u.uriInfos))
	for uri, info := range u.uriInfos {
		uris = append(uris, uri)
		scores[uri] = info.computeScore()
	}
	// Pre-shuffle to avoid overloading first URI when no request are in-flight
	rand.Shuffle(len(uris), func(i, j int) {
		uris[i], uris[j] = uris[j], uris[i]
	})
	sort.Slice(uris, func(i, j int) bool {
		return scores[uris[i]] < scores[uris[j]]
	})
	return uris
}

func (u *balancedScorer) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	baseUri := getBaseURI(req.URL)
	info, foundInfo := u.uriInfos[baseUri]
	if foundInfo {
		atomic.AddInt32(&info.inflight, 1)
		defer atomic.AddInt32(&info.inflight, -1)
	}
	resp, err := next.RoundTrip(req)
	if resp == nil || err != nil {
		return nil, err
	}
	if foundInfo {
		statusCode := resp.StatusCode
		if isGlobalQosStatus(statusCode) || isServerErrorRange(statusCode) {
			info.recentFailures.Update(failureWeight)
		} else if isClientError(statusCode) {
			info.recentFailures.Update(failureWeight / 100)
		}
	}
	return resp, nil
}

func (i *uriInfo) computeScore() int32 {
	return atomic.LoadInt32(&i.inflight) + int32(math.Round(i.recentFailures.Get()))
}

func getBaseURI(u *url.URL) string {
	uCopy := url.URL{
		Scheme: u.Scheme,
		Opaque: u.Opaque,
		User:   u.User,
		Host:   u.Host,
	}
	return uCopy.String()
}

func isGlobalQosStatus(statusCode int) bool {
	return statusCode == StatusCodeRetryOther || statusCode == StatusCodeUnavailable
}

func isServerErrorRange(statusCode int) bool {
	return statusCode/100 == 5
}

func isClientError(statusCode int) bool {
	return statusCode/100 == 4
}
