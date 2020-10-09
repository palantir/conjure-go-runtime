// Copyright (c) 2020 Palantir Technologies. All rights reserved.
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

package errors

// InternalErrorType provides high-level categories for the various conjure error codes.
type InternalErrorType string

const (
	// QOS groups error codes that may indicate a Quality-of-Service problem,
	// such as HTTP 429, 503, etc in the case of HTTP transport.
	QOS InternalErrorType = "qos"

	// Internal groups error codes that are not explicitly defined server-side conjure errors or RPC related.
	Internal InternalErrorType = "internal"

	// ServiceInternal groups the error codes that are explicitly defined server-side conjure errors.
	ServiceInternal InternalErrorType = "service_internal"

	// RPC groups all remote/server-side error codes that are not otherwise ServiceInternal errors.
	RPC InternalErrorType = "rpc"

	// Other is the default catch-all for error codes that don not fall into any other category.
	Other InternalErrorType = "other"

	InternalErrorTypeParam = "_internalErrorType"
	InternalErrorIDParam   = "_internalErrorIdentifier"
)
