package httpclient

import (
	"net/url"
	"time"

	"github.com/palantir/pkg/refreshable"
	werror "github.com/palantir/witchcraft-go-error"
)

const (
	defaultDialTimeout           = 5 * time.Second
	defaultHTTPTimeout           = 60 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultExpectContinueTimeout = 1 * time.Second
	defaultMaxIdleConns          = 200
	defaultMaxIdleConnsPerHost   = 100
)

func newRefreshableURL(refreshableString refreshable.String) (refreshable.Refreshable, error) {
	return refreshable.MapValidatingRefreshable(refreshableString,
		func(i interface{}) (interface{}, error) {
			if s := i.(string); s != "" {
				u, err := url.Parse(s)
				if err != nil {
					return nil, werror.Wrap(err, "invalid proxy url")
				}
				switch u.Scheme {
				case "http", "https", "socks5", "socks5h":
				default:
					return nil, werror.Wrap(err, "invalid proxy url scheme")
				}
			}
			return nil, nil
		},
	)
}
