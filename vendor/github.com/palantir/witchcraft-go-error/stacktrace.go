package werror

import (
	"fmt"
	"runtime"

	"github.com/palantir/witchcraft-go-error/internal/errors"
)

var _ StackTrace = (*stack)(nil)

// StackTrace provides formatting for an underlying stack trace.
type StackTrace interface {
	fmt.Formatter
}

// StackTracer provides the behavior necessary to retrieve a StackTrace formatter.
type StackTracer interface {
	StackTrace() StackTrace
}

// NewStackTrace creates a new StackTrace, constructed by collecting program counters from runtime callers.
func NewStackTrace() StackTrace {
	return NewStackTraceWithSkip(1)
}

// NewStackTraceWithSkip creates a new StackTrace that skips an additional `skip` stack frames.
func NewStackTraceWithSkip(skip int) StackTrace {
	const depth = 32
	var pcs [depth]uintptr
	// only modification is changing "3" to "4" here. Because the stack trace is always taken by the werror package,
	// omit one extra frame (caller should not see werror package as part of the output stack).
	n := runtime.Callers(skip+4, pcs[:])
	var st stack = pcs[0:n]
	return &st
}

// stack represents a stack of program counters.
type stack []uintptr

func (s *stack) Format(state fmt.State, verb rune) {
	switch verb {
	case 'v':
		switch {
		case state.Flag('+'):
			for _, pc := range *s {
				f := errors.Frame(pc)
				_, _ = fmt.Fprintf(state, "\n%+v", f)
			}
		}
	}
}

func (s *stack) StackTrace() errors.StackTrace {
	f := make([]errors.Frame, len(*s))
	for i := 0; i < len(f); i++ {
		f[i] = errors.Frame((*s)[i])
	}
	return f
}
