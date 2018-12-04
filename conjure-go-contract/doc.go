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

// Package contract and its subpackages provides implementation of
// types defined in the Conjure specification.
//
// These types are meant to be used in the Conjure generated code
// as well as in the hand written Conjure-compliant code.
//
// The output of a Conjure generator should not contain client/server logic,
// instead it should just comply with a contract.
//
// In the case of Conjure services, it should just convey a minimal representation of the user defined API.
// This allows consumers to upgrade/swap out their client implementation,
// as long as the new client conforms to the same contract.
//
// See https://github.com/palantir/conjure/blob/develop/docs/spec for
// the actual Conjure specification.
package contract
