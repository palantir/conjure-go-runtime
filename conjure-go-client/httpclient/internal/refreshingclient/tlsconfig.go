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

//import (
//	"crypto/tls"
//	"crypto/x509"
//	"io/ioutil"
//
//	"github.com/palantir/pkg/refreshable"
//	"github.com/palantir/pkg/tlsconfig"
//	werror "github.com/palantir/witchcraft-go-error"
//)
//
//type TLSParams struct {
//	KeyPEMFile           string
//	KeyPEMContent        string
//	ClientCertPEMFile    string
//	ClientCertPEMContent string
//	CACertPEMFiles       []string
//	CACertPEMContent     string
//	InsecureSkipVerify   bool
//}
//
//type RefreshableTLSParams struct {
//	refreshable.Refreshable // contains TLSParams
//}
//
//func (r RefreshableTLSParams) CurrentTLSParams() TLSParams {
//	return r.Current().(TLSParams)
//}
//
//type RefreshableTLSConfig struct {
//	config    *refreshable.ValidatingRefreshable // contains *tls.Config
//	configure []func(cfg *tls.Config) error
//}
//
//func (r *RefreshableTLSConfig) CurrentTLSConfig() (*tls.Config, error) {
//	return r.config.Current().(*tls.Config), r.config.LastValidateErr()
//}
//
//// ConfigureTLSConfig stores a mutator function to apply to generated tls configurations.
//// It should update fields in-place, have no side effects, and not store anything from the cfg which may change.
//func (r *RefreshableTLSConfig) ConfigureTLSConfig(configure func(cfg *tls.Config) error) error {
//	// Store for future configs
//	r.configure = append(r.configure, configure)
//	// Apply to current config
//	if curr, _ := r.CurrentTLSConfig(); curr != nil {
//		if err := configure(curr); err != nil {
//			return err
//		}
//	}
//	return nil
//}
//
//var defaultTLSConfig, _ = tlsconfig.NewClientConfig()
//
//func NewRefreshableTLSConfig(params RefreshableTLSParams) (r *RefreshableTLSConfig, err error) {
//	r = &RefreshableTLSConfig{}
//	r.config, err = refreshable.NewMapValidatingRefreshable(params, func(i interface{}) (interface{}, error) {
//		tlsConfig, err := NewTLSConfig(i.(TLSParams))
//		if err != nil {
//			return nil, err
//		}
//		for _, configure := range r.configure {
//			if err := configure(tlsConfig); err != nil {
//				return nil, err
//			}
//		}
//		return tlsConfig, nil
//	})
//	return r, err
//}
//
//func NewTLSConfig(params TLSParams) (*tls.Config, error) {
//	var tlsParams []tlsconfig.ClientParam
//
//	tlsConfig := defaultTLSConfig.Clone()
//
//	if len(params.CACertPEMContent) > 0 {
//		tlsConfig.RootCAs = x509.NewCertPool()
//		if !tlsConfig.RootCAs.AppendCertsFromPEM([]byte(params.CACertPEMContent)) {
//			return nil, werror.Error("no certificates found in CACertPEMContent")
//		}
//	} else if len(params.CACertPEMFiles) > 0 {
//		var err error
//		tlsConfig.RootCAs, err = tlsconfig.CertPoolFromCAFiles(params.CACertPEMFiles...)()
//		if err != nil {
//			return nil, werror.Wrap(err, "invalid CA certificate files")
//		}
//		tlsParams = append(tlsParams, tlsconfig.ClientRootCAFiles(params.CACertPEMFiles...))
//	}
//
//	var certBytes []byte
//	if len(params.ClientCertPEMContent) > 0 {
//		certBytes = []byte(params.ClientCertPEMContent)
//	} else if len(params.ClientCertPEMFile) > 0 {
//		var err error
//		certBytes, err = ioutil.ReadFile(params.ClientCertPEMFile)
//		if err != nil {
//			return nil, werror.Wrap(err, "invalid client certificate file")
//		}
//	}
//
//	var keyBytes []byte
//	if len(params.KeyPEMContent) > 0 {
//		keyBytes = []byte(params.KeyPEMContent)
//	} else if len(params.KeyPEMFile) > 0 {
//		var err error
//		keyBytes, err = ioutil.ReadFile(params.KeyPEMFile)
//		if err != nil {
//			return nil, werror.Wrap(err, "invalid client key file")
//		}
//	}
//
//	switch {
//	case certBytes != nil && keyBytes != nil:
//		cert, err := tls.X509KeyPair(certBytes, keyBytes)
//		if err != nil {
//			return nil, err
//		}
//		tlsConfig.Certificates = append(tlsConfig.Certificates, cert)
//	case certBytes == nil && keyBytes == nil:
//		// do nothing
//	default:
//		return nil, werror.Error("must set both client certificate and key")
//	}
//
//	tlsConfig.InsecureSkipVerify = params.InsecureSkipVerify
//
//	return tlsConfig, nil
//}
