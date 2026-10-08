//go:build integration

package apt

import (
	"context"
	"testing"
)

// Explicitly opt-in: only read-only commands through the bounded ExecRunner.
func TestLiveSimulation(t *testing.T) {
	got, err := New(nil).Simulate(context.Background(), []string{"bash", "git", "neovim"})
	if got.Simulation.Command == "" || got.Simulation.Err != nil {
		t.Fatalf("simulation failed: %v / %v", got.Simulation.Err, err)
	}
	counts := map[string]int{}
	for _, op := range got.Operations {
		counts[op.Kind]++
	}
	if counts["install"] == 0 || counts["configuration"] != counts["install"]+counts["upgrade"] {
		t.Fatalf("live operations not retained: %v / %v", counts, err)
	}
	if (err != nil) != got.Unresolved {
		t.Fatal("inconsistent resolution state")
	}
	t.Logf("%s; operations=%v unresolved=%t diagnostics=%d", got.Simulation.Command, counts, got.Unresolved, len(got.Metadata))
}
