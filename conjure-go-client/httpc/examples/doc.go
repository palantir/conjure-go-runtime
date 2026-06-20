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

// Package examples contains runnable, self-contained usage examples for the
// httpc HTTP client. Each example lives in its own file as a Go Example
// function so it renders in godoc, runs under go test, and is easy to link to.
//
// Endpoints are values: an httpc.Endpoint describes the HTTP shape of one call
// and is safe to share across goroutines. In real code they are usually
// package-level vars, declared once and reused. The examples instead declare
// them in a var block at the top of each function so every example reads
// top-to-bottom as a single, self-contained unit.
package examples
