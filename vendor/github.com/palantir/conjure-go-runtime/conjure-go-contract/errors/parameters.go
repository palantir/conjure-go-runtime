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

package errors

import (
	"encoding/json"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
)

// Param represents error parameter.
type Param interface {
	apply(*parameterizer)
}

type paramFunc func(*parameterizer)

func (p paramFunc) apply(e *parameterizer) { p(e) }

// SafeParam creates safe error parameter with the given key and value.
func SafeParam(key string, val interface{}) Param {
	return paramFunc(func(e *parameterizer) {
		if e.safe == nil {
			e.safe = map[string]interface{}{}
		}
		e.safe[key] = val
	})
}

// UnsafeParam creates unsafe error parameter with the given key and value.
func UnsafeParam(key string, val interface{}) Param {
	return paramFunc(func(e *parameterizer) {
		if e.unsafe == nil {
			e.unsafe = map[string]interface{}{}
		}
		e.unsafe[key] = val
	})
}

func newParameterizer(parameters ...Param) parameterizer {
	var p parameterizer
	for _, param := range parameters {
		param.apply(&p)
	}
	return p
}

// parameterizer is generic container for storing parameters of any type.
type parameterizer struct {
	safe   map[string]interface{}
	unsafe map[string]interface{}
}

var (
	_ json.Marshaler   = parameterizer{}
	_ json.Unmarshaler = &parameterizer{}
)

func (p parameterizer) Parameters() map[string]interface{} {
	all := make(map[string]interface{}, len(p.safe)+len(p.unsafe))
	for k, v := range p.safe {
		all[k] = v
	}
	for k, v := range p.unsafe {
		all[k] = v
	}
	return all
}

func (p parameterizer) MarshalJSON() ([]byte, error) {
	return codecs.JSON.Marshal(struct {
		Safe   map[string]interface{} `json:"safe,omitempty"`
		Unsafe map[string]interface{} `json:"unsafe,omitempty"`
	}{
		Safe:   p.safe,
		Unsafe: p.unsafe,
	})
}

func (p *parameterizer) UnmarshalJSON(data []byte) (err error) {
	// TODO: Serialization of interface{} is not reliable, e.g. int(123) can be unmarshalled into float64(123.0)
	// TODO: Given that we can't deserialize parameters into correct types consider unmarshalling them into map[string]json.RawMessage.

	// First trying to unmarshal into default representation
	// which supports safe and unsafe parameters.
	var unmarshalled struct {
		Safe   map[string]interface{} `json:"safe"`
		Unsafe map[string]interface{} `json:"unsafe"`
	}
	if err = codecs.JSON.Unmarshal(data, &unmarshalled); err == nil {
		p.safe = unmarshalled.Safe
		p.unsafe = unmarshalled.Unsafe
		return nil
	}
	// Falling back and  trying to unmarshal all parameters into unsafe map.
	unsafe := map[string]interface{}{}
	if err = codecs.JSON.Unmarshal(data, &unsafe); err == nil {
		p.safe = nil
		p.unsafe = unsafe
		return nil
	}
	return err
}
