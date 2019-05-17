// Copyright (c) 2019 Palantir Technologies. All rights reserved.
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

package httpclient_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/palantir/conjure-go-runtime/conjure-go-client/httpclient"
)

func TestNoBaseURIs(t *testing.T) {
	client, err := httpclient.NewClient()
	require.NoError(t, err)

	_, err = client.Do(context.Background(), httpclient.WithRequestMethod("GET"))
	require.Error(t, err)
}
