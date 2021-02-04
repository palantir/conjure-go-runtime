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

package httpclient

import (
	"fmt"
	"time"
)

func (b *httpClientBuilder) handleIdleConnUpdate(c *clientImpl) {
	errs := make(chan error, 1)
	unsubscribeIdleConn := b.IdleConnTimeout.SubscribeToDuration(func(_ time.Duration) {
		err := rebuildClient(c, b)
		if err != nil {
			errs <- err
		}
	})
	go func() {
		select {
		case err := <-errs:
			// TODO: Find a way to pass this back to the service log
			fmt.Printf("encountered an error whilst updating IdleConn: %s\n", err)
			unsubscribeIdleConn()
			close(errs)
			return
		case <-b.ctx.Done():
			close(errs)
			return
		}
	}()
}

func (b *httpClientBuilder) handleTLSHandshakeTimeoutUpdate(c *clientImpl) {
	errs := make(chan error, 1)
	unsubscribeTLSHandshakeTimeout := b.TLSHandshakeTimeout.SubscribeToDuration(func(_ time.Duration) {
		err := rebuildClient(c, b)
		if err != nil {
			errs <- err
		}
	})
	go func() {
		select {
		case err := <-errs:
			// TODO: Find a way to pass this back to the service log
			fmt.Printf("encountered an error whilst updating TLSHandshakeTimeout: %s\n", err)
			unsubscribeTLSHandshakeTimeout()
			close(errs)
			return
		case <-b.ctx.Done():
			close(errs)
			return
		}
	}()
}

func (b *httpClientBuilder) handleExpectContinueTimeoutUpdate(c *clientImpl) {
	errs := make(chan error, 1)
	unsubscribeExpectContinueTimeout := b.ExpectContinueTimeout.SubscribeToDuration(func(_ time.Duration) {
		err := rebuildClient(c, b)
		if err != nil {
			errs <- err
		}
	})
	go func() {
		select {
		case err := <-errs:
			// TODO: Find a way to pass this back to the service log
			fmt.Printf("encountered an error whilst updating ExpectContinueTimeout: %s\n", err)
			unsubscribeExpectContinueTimeout()
			close(errs)
			return
		case <-b.ctx.Done():
			close(errs)
			return
		}
	}()
}

func (b *httpClientBuilder) handleDialTimeoutUpdate(c *clientImpl) {
	errs := make(chan error, 1)
	unsubscribeDialTimeout := b.DialTimeout.SubscribeToDuration(func(_ time.Duration) {
		err := rebuildClient(c, b)
		if err != nil {
			errs <- err
		}
	})
	go func() {
		select {
		case err := <-errs:
			// TODO: Find a way to pass this back to the service log
			fmt.Printf("encountered an error whilst updating DialTimeout: %s\n", err)
			unsubscribeDialTimeout()
			close(errs)
			return
		case <-b.ctx.Done():
			close(errs)
			return
		}
	}()
}

func (b *httpClientBuilder) handleMaxIdleConnsUpdate(c *clientImpl) {
	errs := make(chan error, 1)
	unsubscribeMaxIdleConns := b.MaxIdleConns.SubscribeToInt(func(_ int) {
		err := rebuildClient(c, b)
		if err != nil {
			errs <- err
		}
	})
	go func() {
		select {
		case err := <-errs:
			// TODO: Find a way to pass this back to the service log
			fmt.Printf("encountered an error whilst updating MaxIdleConns: %s\n", err)
			unsubscribeMaxIdleConns()
			close(errs)
			return
		case <-b.ctx.Done():
			close(errs)
			return
		}
	}()
}

func (b *httpClientBuilder) handleMaxIdleConnsPerHostUpdate(c *clientImpl) {
	errs := make(chan error, 1)
	unsubscribeMaxIdleConnsPerHost := b.MaxIdleConnsPerHost.SubscribeToInt(func(_ int) {
		err := rebuildClient(c, b)
		if err != nil {
			errs <- err
		}
	})
	go func() {
		select {
		case err := <-errs:
			// TODO: Find a way to pass this back to the service log
			fmt.Printf("encountered an error whilst updating MaxIdleConnsPerHost: %s\n", err)
			unsubscribeMaxIdleConnsPerHost()
			close(errs)
			return
		case <-b.ctx.Done():
			close(errs)
			return
		}
	}()
}

func rebuildClient(c *clientImpl, b *httpClientBuilder) error {
	nc, nm, err := httpClientAndRoundTripHandlersFromBuilder(b)
	if err != nil {
		return err
	}
	c.client = *nc
	c.middlewares = nm
	return nil
}
