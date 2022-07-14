package httpclient

import (
	"bytes"
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"github.com/palantir/witchcraft-go-logging/wlog"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestErrorRegistry_LogError(t *testing.T) {
	myRegistry := NewErrorRegistry(GetGenericErrorLoggerWithType(UnknownAuthorityErrorLogger()))
	notHandledError := fmt.Errorf("i won't get logged")
	handledError := unknownAuthorityError()
	out := &bytes.Buffer{}
	wlog.SetDefaultLoggerProvider(wlog.NewJSONMarshalLoggerProvider())
	ctx := svc1log.WithLogger(context.Background(), svc1log.New(out, wlog.DebugLevel))
	myRegistry.LogError(ctx, notHandledError)
	assert.Empty(t, out.String())
	myRegistry.LogError(ctx, handledError)
	assert.NotEmpty(t, out.String())
	assert.Contains(t, out.String(), "common name")
}

func unknownAuthorityError() error {
	return x509.UnknownAuthorityError{
		Cert: &x509.Certificate{
			Subject: pkix.Name{
				CommonName: "common name",
			},
		},
	}
}
