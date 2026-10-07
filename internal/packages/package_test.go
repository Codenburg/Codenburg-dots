package packages

import "testing"

func TestDeriveState(t *testing.T) {
	cases := []struct {
		installed, candidate             string
		found, installedAtLeastCandidate bool
		want                             Status
	}{
		{"", "2", true, false, Available}, {"1", "2", true, false, UpdateAvailable}, {"2", "1", true, true, Current},
		{"", "", false, false, Unavailable}, {"1", "", false, false, Unknown},
	}
	for _, tc := range cases {
		got := DeriveState(tc.installed, tc.candidate, tc.found, tc.installedAtLeastCandidate)
		if got != tc.want {
			t.Errorf("DeriveState(%q,%q,%v) = %q; want %q", tc.installed, tc.candidate, tc.found, got, tc.want)
		}
	}
}
