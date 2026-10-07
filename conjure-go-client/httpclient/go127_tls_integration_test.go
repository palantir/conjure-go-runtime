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

package httpclient_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
	"github.com/palantir/pkg/metrics"
	"github.com/stretchr/testify/require"
)

const (
	signatureAlgorithmsCertExtension = 0x0032
	supportedVersionsExtension       = 0x002b
	mldsa44                          = 0x0904
	mldsa65                          = 0x0905
	mldsa87                          = 0x0906
)

// TestGo127TLS13HandshakeRegression reproduces golang/go#79626 and
// golang/go#81199 without relying on an external service. The server models
// the non-compliant middlebox from those issues: it stops responding when a
// TLS 1.3 ClientHello advertises an ML-DSA algorithm in the
// signature_algorithms_cert extension.
//
// Go 1.26 does not advertise those algorithms and completes TLS 1.3. Go 1.27
// advertises them, times out, and falls back to a new TLS 1.2 connection.
// Restricting either toolchain to TLS 1.2 succeeds and provides a control case.
func TestGo127TLS13HandshakeRegression(t *testing.T) {
	goVersion := runtime.Version()
	isGo126 := strings.HasPrefix(goVersion, "go1.26")
	isGo127 := strings.HasPrefix(goVersion, "go1.27")
	require.True(t, isGo126 || isGo127, "reproducer must be built with Go 1.26 or 1.27, got %s", goVersion)

	t.Run("TLS 1.3", func(t *testing.T) {
		server := newClientHelloIntolerantServer(t)
		resp, err := requestInMemoryServer(t, server.URL(), 0)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.TLS)
		if isGo127 {
			require.Equal(t, uint16(tls.VersionTLS12), resp.TLS.Version)
			require.True(t, <-server.rejectedClientHello, "server did not observe the incompatible ClientHello")
			require.False(t, <-server.rejectedClientHello, "server rejected the TLS 1.2 fallback ClientHello")
			return
		}

		require.Equal(t, uint16(tls.VersionTLS13), resp.TLS.Version)
		require.False(t, <-server.rejectedClientHello)
	})

	t.Run("fallback disabled", func(t *testing.T) {
		server := newClientHelloIntolerantServer(t)
		resp, err := requestInMemoryServer(t, server.URL(), 0, httpclient.WithDisableTLS12FallbackOnTimeout())
		if isGo127 {
			require.Error(t, err)
			require.Nil(t, resp)
			require.Contains(t, err.Error(), "TLS handshake timeout")
			require.True(t, <-server.rejectedClientHello)
			return
		}

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, uint16(tls.VersionTLS13), resp.TLS.Version)
		require.False(t, <-server.rejectedClientHello)
	})

	t.Run("TLS 1.2 control", func(t *testing.T) {
		server := newClientHelloIntolerantServer(t)
		resp, err := requestInMemoryServer(t, server.URL(), tls.VersionTLS12)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.TLS)
		require.Equal(t, uint16(tls.VersionTLS12), resp.TLS.Version)
		require.False(t, <-server.rejectedClientHello)
	})
}

func TestGo127TLSFallbackMetric(t *testing.T) {
	if !strings.HasPrefix(runtime.Version(), "go1.27") {
		t.Skip("Go 1.27 ClientHello is required to exercise the fallback")
	}

	server := newClientHelloIntolerantServer(t)
	client, err := httpclient.NewClient(
		httpclient.WithBaseURLs([]string{server.URL()}),
		httpclient.WithDisableRestErrors(),
		httpclient.WithMaxRetries(0),
		httpclient.WithTLSHandshakeTimeout(250*time.Millisecond),
		httpclient.WithTLSInsecureSkipVerify(),
		httpclient.WithNoProxy(),
		httpclient.WithServiceName("tls-fallback-test"),
	)
	require.NoError(t, err)

	registry := metrics.NewRootMetricsRegistry()
	ctx := metrics.WithRegistry(context.Background(), registry)
	resp, err := client.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, uint16(tls.VersionTLS12), resp.TLS.Version)
	require.Equal(t, int64(1), registry.Meter(
		httpclient.MetricTLSFallback,
		metrics.MustNewTag(httpclient.MetricTagServiceName, "tls-fallback-test"),
	).Count())
}

func requestInMemoryServer(t *testing.T, url string, maxTLSVersion uint16, additionalParams ...httpclient.ClientParam) (*http.Response, error) {
	t.Helper()

	params := []httpclient.ClientParam{
		httpclient.WithBaseURLs([]string{url}),
		httpclient.WithDisableRestErrors(),
		httpclient.WithMaxRetries(0),
		httpclient.WithTLSHandshakeTimeout(250 * time.Millisecond),
		httpclient.WithTLSInsecureSkipVerify(),
		httpclient.WithNoProxy(),
	}
	if maxTLSVersion != 0 {
		params = append(params, httpclient.WithTLSMaxVersion(maxTLSVersion))
	}
	params = append(params, additionalParams...)
	client, err := httpclient.NewClient(params...)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return client.Get(ctx)
}

type clientHelloIntolerantServer struct {
	listener            net.Listener
	rejectedClientHello chan bool
}

func newClientHelloIntolerantServer(t *testing.T) *clientHelloIntolerantServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &clientHelloIntolerantServer{
		listener:            listener,
		rejectedClientHello: make(chan bool, 2),
	}
	t.Cleanup(func() { require.NoError(t, listener.Close()) })

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{newServerCertificate(t)},
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   tls.VersionTLS13,
	}
	go server.serve(tlsConfig)
	return server
}

func (s *clientHelloIntolerantServer) URL() string {
	return "https://" + s.listener.Addr().String()
}

func (s *clientHelloIntolerantServer) serve(tlsConfig *tls.Config) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.serveConnection(conn, tlsConfig)
	}
}

func (s *clientHelloIntolerantServer) serveConnection(conn net.Conn, tlsConfig *tls.Config) {
	defer func() {
		_ = conn.Close()
	}()

	record, err := readTLSRecord(conn)
	if err != nil {
		return
	}
	reject := clientHelloOffersTLS13(record) && clientHelloAdvertisesMLDSACertSignature(record)
	s.rejectedClientHello <- reject
	if reject {
		// Model the affected middleboxes: keep the TCP connection open but do
		// not send a ServerHello. The client's TLS handshake timer closes it.
		_, _ = io.Copy(io.Discard, conn)
		return
	}

	tlsConn := tls.Server(&replayConn{Conn: conn, reader: io.MultiReader(bytes.NewReader(record), conn)}, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		return
	}
	if _, err := http.ReadRequest(bufio.NewReader(tlsConn)); err != nil {
		return
	}
	_, _ = io.WriteString(tlsConn, "HTTP/1.1 204 No Content\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
}

type replayConn struct {
	net.Conn
	reader io.Reader
}

func (c *replayConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func readTLSRecord(r io.Reader) ([]byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	if header[0] != 22 {
		return nil, fmt.Errorf("expected TLS handshake record, got record type %d", header[0])
	}
	body := make([]byte, int(binary.BigEndian.Uint16(header[3:5])))
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return append(header, body...), nil
}

func clientHelloOffersTLS13(record []byte) bool {
	extensions, ok := clientHelloExtensions(record)
	if !ok {
		return false
	}
	versions, ok := extensions[supportedVersionsExtension]
	if !ok || len(versions) < 1 || int(versions[0])+1 > len(versions) {
		return false
	}
	for versions = versions[1 : 1+versions[0]]; len(versions) >= 2; versions = versions[2:] {
		if binary.BigEndian.Uint16(versions[:2]) == tls.VersionTLS13 {
			return true
		}
	}
	return false
}

func clientHelloAdvertisesMLDSACertSignature(record []byte) bool {
	extensions, ok := clientHelloExtensions(record)
	if !ok {
		return false
	}
	algorithms, ok := extensions[signatureAlgorithmsCertExtension]
	if !ok || len(algorithms) < 2 || int(binary.BigEndian.Uint16(algorithms[:2]))+2 > len(algorithms) {
		return false
	}
	for algorithms = algorithms[2 : 2+binary.BigEndian.Uint16(algorithms[:2])]; len(algorithms) >= 2; algorithms = algorithms[2:] {
		switch binary.BigEndian.Uint16(algorithms[:2]) {
		case mldsa44, mldsa65, mldsa87:
			return true
		}
	}
	return false
}

func clientHelloExtensions(record []byte) (map[uint16][]byte, bool) {
	if len(record) < 5+4 || record[5] != 1 {
		return nil, false
	}
	hello := record[9:]
	if len(hello) < 2+32+1 {
		return nil, false
	}
	hello = hello[2+32:]
	if !skipVector(&hello, 1) || !skipVector(&hello, 2) || !skipVector(&hello, 1) || len(hello) < 2 {
		return nil, false
	}
	extensionsLength := int(binary.BigEndian.Uint16(hello[:2]))
	hello = hello[2:]
	if extensionsLength > len(hello) {
		return nil, false
	}
	extensions := make(map[uint16][]byte)
	for remaining := hello[:extensionsLength]; len(remaining) >= 4; {
		extensionType := binary.BigEndian.Uint16(remaining[:2])
		extensionLength := int(binary.BigEndian.Uint16(remaining[2:4]))
		remaining = remaining[4:]
		if extensionLength > len(remaining) {
			return nil, false
		}
		extensions[extensionType] = remaining[:extensionLength]
		remaining = remaining[extensionLength:]
	}
	return extensions, true
}

func skipVector(data *[]byte, lengthBytes int) bool {
	if len(*data) < lengthBytes {
		return false
	}
	var length int
	switch lengthBytes {
	case 1:
		length = int((*data)[0])
	case 2:
		length = int(binary.BigEndian.Uint16((*data)[:2]))
	default:
		panic("unsupported vector length")
	}
	if len(*data) < lengthBytes+length {
		return false
	}
	*data = (*data)[lengthBytes+length:]
	return true
}

func newServerCertificate(t *testing.T) tls.Certificate {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "in-memory TLS regression server"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER}),
	)
	require.NoError(t, err)
	return certificate
}
