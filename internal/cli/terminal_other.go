//go:build !linux

package cli

import (
	"context"
	"errors"
	"os"
)

func terminalCapable(any) bool { return false }
func terminalLine(context.Context, *os.File) (string, error) {
	return "", errors.New("interactive apply requires Linux terminal support")
}
