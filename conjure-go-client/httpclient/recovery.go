// Copyright (c) 2018 Palantir Technologies. All rights reserved.
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
	"fmt"
	"net/http"

	"github.com/palantir/pkg/refreshable"
	werror "github.com/palantir/witchcraft-go-error"
)

// recoveryMiddleware recovers panics encountered during the request and returns them as an error.
type recoveryMiddleware struct {
	Disabled refreshable.Bool
}

func (h recoveryMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (resp *http.Response, err error) {
	if h.Disabled != nil && h.Disabled.CurrentBool() {
		// If we have a Disabled refreshable and it is true, no-op.
		return next.RoundTrip(req)
	}

	defer func() {
		if r := recover(); r != nil {
			// panics contain function arguments (like maybe auth tokens), so we must log them unsafe.
			if err == nil {
				err = werror.Error("recovered panic", werror.UnsafeParam("recovered", fmt.Sprintf("%v", r)))
			} else {
				err = werror.Wrap(err, "recovered panic", werror.UnsafeParam("recovered", fmt.Sprintf("%v", r)))
			}
		}
	}()

	return next.RoundTrip(req)
}
