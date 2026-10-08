package apt

import (
	"strings"
	"testing"
)

func TestBoundedOutputRetainsPrefixAndDrains(t *testing.T) {
	var b boundedOutput
	input := strings.Repeat("a", maxCommandOutput) + "overflow"
	n, err := b.Write([]byte(input))
	if err != nil || n != len(input) || !b.exceeded || b.Len() != maxCommandOutput {
		t.Fatalf("write=%d err=%v length=%d", n, err, b.Len())
	}
	n, err = b.Write([]byte("more"))
	if err != nil || n != 4 || b.Len() != maxCommandOutput {
		t.Fatalf("did not drain %d %v", n, err)
	}
	if b.String() != input[:maxCommandOutput] {
		t.Fatal("prefix corrupted")
	}
}

func TestCLocaleEnvironmentOverridesExistingValue(t *testing.T) {
	t.Setenv("LC_ALL", "other")
	count := 0
	for _, v := range cLocaleEnvironment() {
		if strings.HasPrefix(v, "LC_ALL=") {
			count++
			if v != "LC_ALL=C" {
				t.Fatal(v)
			}
		}
	}
	if count != 1 {
		t.Fatalf("LC_ALL entries=%d", count)
	}
}
