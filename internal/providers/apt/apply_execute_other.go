//go:build !linux

package apt

import (
	"context"
	"errors"
	"io"
)

// Executor is unavailable outside Linux; construction performs no inspection.
type Executor struct{}

func NewExecutor(_ *Provider, _ io.Reader, _, _ io.Writer) *Executor { return &Executor{} }

func (*Executor) Execute(context.Context, Preview) error {
	return errors.New("guarded APT execution requires Linux")
}

func RunInternalGuard([]string, io.Reader) error {
	return errors.New("native APT guard requires Linux")
}
