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

package b3

import (
	"net/http"
	"strings"

	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-tracing/wtracing"
)

// SpanExtractor returns a SpanExtractor that returns a wtracing.SpanContext based on the header content of the provided
// *http.Request. If the values in the provided header do not constitute a valid SpanContext, for example if it has a
// SpanID but no TraceID, or an unsupported Sampled value, the Err field of the returned SpanContext will be non-nil
// and will contain an error that describes why the values were invalid. However, even if the Err field is set, all
// of the values that could be extracted from the header are still set on the returned context.
//
// A request is allowed to leave out TraceID and SpanID entirely and carry only a sampling decision, for example a
// lone X-B3-Sampled header. The B3 propagation spec treats that as valid, see
// https://github.com/openzipkin/b3-propagation#sampling-state, so it is not treated as an error here either, unless
// a ParentSpanId is also present, since a parent span cannot exist without a trace to belong to.
func SpanExtractor(req *http.Request) wtracing.SpanExtractor {
	return func() wtracing.SpanContext {
		var sc wtracing.SpanContext
		var errMsgs []string
		errSafeParams := make(map[string]any)

		traceID := strings.ToLower(req.Header.Get(b3TraceID))
		sc.TraceID = wtracing.TraceID(traceID)

		spanID := strings.ToLower(req.Header.Get(b3SpanID))
		sc.ID = wtracing.SpanID(spanID)

		parentID := strings.ToLower(req.Header.Get(b3ParentSpanID))
		if parentID != "" {
			sc.ParentID = (*wtracing.SpanID)(&parentID)
		}

		errMsgPrefix := ""
		if parentID != "" {
			errMsgPrefix = "ParentID present but "
		}
		switch {
		case traceID == "" && spanID == "":
			if parentID != "" {
				errMsgs = append(errMsgs, errMsgPrefix+"TraceID and SpanID missing")
			}
		case traceID == "":
			errMsgs = append(errMsgs, errMsgPrefix+"TraceID missing")
		case spanID == "":
			errMsgs = append(errMsgs, errMsgPrefix+"SpanID missing")
		}

		var sampledVal *bool
		switch sampledHeader := strings.ToLower(req.Header.Get(b3Sampled)); sampledHeader {
		case falseHeaderVal, "false":
			boolVal := false
			sampledVal = &boolVal
		case trueHeaderVal, "true":
			boolVal := true
			sampledVal = &boolVal
		case "":
			// keep nil
		default:
			errMsgs = append(errMsgs, "Sampled invalid")
			errSafeParams["sampledHeaderVal"] = sampledHeader
		}
		debug := req.Header.Get(b3Flags) == trueHeaderVal
		if debug {
			sampledVal = nil
		}
		sc.Sampled = sampledVal
		sc.Debug = debug

		if len(errMsgs) > 0 {
			sc.Err = werror.Error(strings.Join(errMsgs, "; "), werror.SafeParams(errSafeParams))
		}
		return sc
	}
}
