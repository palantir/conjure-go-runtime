// Copyright (c) 2026 Palantir Technologies. All rights reserved.
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

// Package snappybody provides an httpc Snappy compression encoder. It is
// intentionally separate from the core httpc package so callers who do not use
// Snappy do not pull in github.com/golang/snappy transitively.
package snappybody

import (
	"github.com/golang/snappy"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// SnappyEncoder wraps inner with Snappy compression and sets Content-Encoding: snappy.
// It is the [httpc.GZIPEncoder]/[httpc.ZLIBEncoder] counterpart for Snappy, built on
// [httpc.CompressedEncoder].
func SnappyEncoder[Req any](inner httpc.BodyEncoder[Req]) httpc.BodyEncoder[Req] {
	return httpc.CompressedEncoder(inner, "snappy", snappy.NewBufferedWriter)
}
