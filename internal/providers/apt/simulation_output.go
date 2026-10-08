package apt

import "strings"

// Keep draining pipes after the limit; an overflow is never successful evidence.
type boundedOutput struct {
	strings.Builder
	exceeded bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := maxCommandOutput - b.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.exceeded = true
	}
	_, err := b.Builder.Write(p)
	return n, err
}
