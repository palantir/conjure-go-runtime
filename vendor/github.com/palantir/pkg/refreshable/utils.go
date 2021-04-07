// Copyright (c) 2021 Palantir Technologies. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package refreshable

import (
	"errors"
	"reflect"
	"sync/atomic"
)

func MapAll(refreshables []Refreshable, mapFn func(vals []interface{}) interface{}) (mapped Refreshable, unsubscribe func()) {
	currentVals := func() []interface{} {
		vals := make([]interface{}, len(refreshables))
		for i, r := range refreshables {
			vals[i] = r.Current()
		}
		return vals
	}

	newRefreshable := NewDefaultRefreshable(mapFn(currentVals()))
	unsubs := make([]func(), len(refreshables))
	for i, r := range refreshables {
		unsubs[i] = r.Subscribe(func(i interface{}) {
			_ = newRefreshable.Update(mapFn(currentVals()))
		})
	}

	unsubscribe = func() {
		for _, unsub := range unsubs {
			if unsub != nil {
				unsub()
			}
		}
	}

	return newRefreshable, unsubscribe
}

func Coalesce(defaultVal interface{}, refreshables ...Refreshable) (r Refreshable, unsubscribe func()) {
	return MapAll(refreshables, func(vals []interface{}) interface{} {
		for _, val := range vals {
			if val != nil && !reflect.ValueOf(val).IsZero() {
				return val
			}
		}
		return defaultVal
	})
}

// MapValidatingRefreshable is similar to NewValidatingRefreshable but allows for the validatingFn to return a mapping/mutation
// of the input object in addition to returning an error. It must always return a value of same type.
func MapValidatingRefreshable(origRefreshable Refreshable, validatingFn func(interface{}) (interface{}, error)) (*ValidatingRefreshable, error) {
	if validatingFn == nil {
		return nil, errors.New("failed to create validating Refreshable because the validating function was nil")
	}

	if origRefreshable == nil {
		return nil, errors.New("failed to create validating Refreshable because the passed in Refreshable was nil")
	}

	mappedVal, err := validatingFn(origRefreshable.Current())
	if err != nil {
		return nil, err
	}

	validatedRefreshable := NewDefaultRefreshable(mappedVal)

	var lastValidateErr atomic.Value
	lastValidateErr.Store(errorWrapper{})
	v := ValidatingRefreshable{
		validatedRefreshable: validatedRefreshable,
		lastValidateErr:      &lastValidateErr,
	}

	origRefreshable.Subscribe(func(i interface{}) {
		mappedVal, err := validatingFn(i)
		if err != nil {
			v.lastValidateErr.Store(errorWrapper{err})
			return
		}
		if err := validatedRefreshable.Update(mappedVal); err != nil {
			v.lastValidateErr.Store(errorWrapper{err})
			return
		}
		v.lastValidateErr.Store(errorWrapper{})
	})
	return &v, nil
}
