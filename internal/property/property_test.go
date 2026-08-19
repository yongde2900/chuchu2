package property

import "testing"

func TestRentalMode_Valid(t *testing.T) {
	cases := []struct {
		mode RentalMode
		want bool
	}{
		{RentalModeMasterLease, true},
		{RentalModeManaged, true},
		{RentalMode("SUBLEASE"), false},
		{RentalMode(""), false},
	}

	for _, tc := range cases {
		t.Run(string(tc.mode), func(t *testing.T) {
			if got := tc.mode.Valid(); got != tc.want {
				t.Fatalf("RentalMode(%q).Valid() = %v, want %v", tc.mode, got, tc.want)
			}
		})
	}
}

func TestStatus_Valid(t *testing.T) {
	cases := []struct {
		status Status
		want   bool
	}{
		{StatusVacant, true},
		{StatusOccupied, true},
		{StatusRenovating, true},
		{StatusDelisted, true},
		{Status("UNKNOWN"), false},
		{Status(""), false},
	}

	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			if got := tc.status.Valid(); got != tc.want {
				t.Fatalf("Status(%q).Valid() = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestLayout_Valid(t *testing.T) {
	cases := []struct {
		layout Layout
		want   bool
	}{
		{LayoutWholeUnit, true},
		{LayoutIndependentSuite, true},
		{LayoutSharedSuite, true},
		{LayoutSingleRoom, true},
		{Layout("CASTLE"), false},
		{Layout(""), false},
	}

	for _, tc := range cases {
		t.Run(string(tc.layout), func(t *testing.T) {
			if got := tc.layout.Valid(); got != tc.want {
				t.Fatalf("Layout(%q).Valid() = %v, want %v", tc.layout, got, tc.want)
			}
		})
	}
}
