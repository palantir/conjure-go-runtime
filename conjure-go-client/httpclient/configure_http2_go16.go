// Copyright (c) 2021 Palantir Technologies. All rights reserved.
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

// +build go1.16

package httpclient

import (
	"net/http"
	"time"

	werror "github.com/palantir/witchcraft-go-error"
	"golang.org/x/net/http2"
)

// configureHTTP2 will attempt to configure net/http HTTP/1 Transport to use HTTP/2.
// The provided readIdleTimeout will set the underlying HTTP/2 transport ReadIdleTimeout.
// It returns an error if t1 has already been HTTP/2-enabled.
func configureHTTP2(t1 *http.Transport, readIdleTimeout time.Duration) error {
	http2Transport, err := http2.ConfigureTransports(t1)
	if err != nil {
		return werror.Wrap(err, "failed to configure transport for http2")
	}
	// ReadIdleTimeout is the timeout after which a health check using ping
	// frame will be carried out if no frame is received on the connection.
	// Setting this value will enable health checks and allows broken idle
	// connections to be pruned more quickly, preventing the client from
	// attempting to re-use connections that will no longer work.
	// ref: https://github.com/golang/go/issues/36026
	http2Transport.ReadIdleTimeout = readIdleTimeout

	return nil
}
