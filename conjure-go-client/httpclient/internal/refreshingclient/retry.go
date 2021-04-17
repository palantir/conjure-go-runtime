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

package refreshingclient

import (
	"context"

	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/pkg/retry"
)

type RefreshableRetryParams struct {
	MaxAttempts    refreshable.IntPtr // 0 means no limit. If nil, uses 2*len(uris).
	InitialBackoff refreshable.Duration
	MaxBackoff     refreshable.Duration
}

func (r RefreshableRetryParams) Start(ctx context.Context) retry.Retrier {
	return retry.Start(ctx,
		retry.WithInitialBackoff(r.InitialBackoff.CurrentDuration()),
		retry.WithMaxBackoff(r.MaxBackoff.CurrentDuration()),
	)
}
