package httpclient

import (
	"context"
	"crypto/x509"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"reflect"
)

type ErrorRegistry interface {
	LogError(ctx context.Context, err error)
}

func GetGenericErrorLoggerWithType[E error](genericErrorLogger GenericErrorLogger[E]) GenericErrorLoggerWithType[E] {
	var e E
	return GenericErrorLoggerWithType[E]{
		Type:        reflect.TypeOf(e),
		ErrorLogger: genericErrorLogger,
	}
}

type GenericErrorLoggerWithType[E error] struct {
	Type        reflect.Type
	ErrorLogger GenericErrorLogger[E]
}

func NewErrorRegistry(loggersWithTypes ...GenericErrorLoggerWithType) ErrorRegistry {
	registry := make(errorRegistry)
	for _, loggerWithType := range loggersWithTypes {
		registry[loggerWithType.Type] = asAnyErrorLogger(loggerWithType.ErrorLogger)
	}
	return registry
}

type errorRegistry map[reflect.Type]AnyErrorLogger

func (e errorRegistry) LogError(ctx context.Context, err error) {
	handler, ok := e[reflect.TypeOf(err)]
	if ok {
		handler.LogError(ctx, err)
	}
}

func asAnyErrorLogger[E error](errorLogger GenericErrorLogger[E]) AnyErrorLogger {
	return genericErrorLoggerFn[error](func(ctx context.Context, err error) {
		errorLogger(ctx, err.(E))
	})
}

type AnyErrorLogger GenericErrorLogger[error]

type GenericErrorLogger[E error] interface {
	LogError(ctx context.Context, err E)
}

type genericErrorLoggerFn[E error] func(ctx context.Context, err E)

func (fn genericErrorLoggerFn[E]) LogError(ctx context.Context, err E) {
	fn(ctx, err)
}

var _ GenericErrorLogger[error] = (genericErrorLoggerFn[error])(nil)

func UnknownAuthorityErrorLogger() GenericErrorLogger[x509.UnknownAuthorityError] {
	return genericErrorLoggerFn[x509.UnknownAuthorityError](func(ctx context.Context, err x509.UnknownAuthorityError) {
		svc1log.FromContext(ctx).Error("Encountered UnknownAuthorityError.", svc1log.SafeParams(map[string]interface{}{
			"certSANs":     err.Cert.DNSNames,
			"certCN":       err.Cert.Subject.CommonName,
			"issuerCertCN": err.Cert.Issuer.CommonName,
			"rawSubject":   err.Cert.RawSubject,
			"rawIssuer":    err.Cert.RawIssuer,
		}), svc1log.Stacktrace(err))
	})
}
