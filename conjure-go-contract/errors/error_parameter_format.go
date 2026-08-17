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

package errors

const (
	// AcceptConjureErrorParameterFormatHeader is the request header a client sends to negotiate
	// the serialization format of Conjure error parameters. When the value is recognized by
	// the server, the server serializes error parameters in that format instead of the legacy form.
	//
	// The header is a best-effort hint: servers that do not understand it ignore it and continue
	// to send the legacy form, so error decoding must remain tolerant of both forms.
	AcceptConjureErrorParameterFormatHeader = "Accept-Conjure-Error-Parameter-Format"
)

// ConjureErrorParameterFormat is the value of the Accept-Conjure-Error-Parameter-Format header.
type ConjureErrorParameterFormat string

const (
	// ConjureErrorParameterFormatJSON requests that the server serialize Conjure error parameters
	// as native JSON values keyed by their declared Conjure type, rather than the legacy string form.
	ConjureErrorParameterFormatJSON ConjureErrorParameterFormat = "JSON"
)
