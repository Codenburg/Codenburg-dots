//go:build !linux

package apt

import (
	"context"
	"errors"
)

// NewApplyProvider is lazy and fails closed outside the established Linux profile.
func NewApplyProvider() *Provider { return New(unsupportedApplyRunner{}) }

type unsupportedApplyRunner struct{}

func (unsupportedApplyRunner) Run(context.Context, string, ...string) (string, string, error) {
	return "", "", errors.New("authoritative APT apply evidence requires Linux")
}
