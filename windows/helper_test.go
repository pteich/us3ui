package windows

import "testing"

func TestByteCountSI(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1000, "1.0 kB"},
		{1500, "1.5 kB"},
		{1000000, "1.0 MB"},
		{1500000, "1.5 MB"},
		{1000000000, "1.0 GB"},
	}

	for _, tt := range tests {
		got := ByteCountSI(tt.input)
		if got != tt.expected {
			t.Errorf("ByteCountSI(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestContains(t *testing.T) {
	slice := []string{"alpha", "beta", "gamma"}

	presentCases := []string{"alpha", "beta", "gamma"}
	for _, s := range presentCases {
		if !contains(slice, s) {
			t.Errorf("contains(%v, %q) = false, want true", slice, s)
		}
	}

	absentCases := []string{"delta", "", "ALPHA"}
	for _, s := range absentCases {
		if contains(slice, s) {
			t.Errorf("contains(%v, %q) = true, want false", slice, s)
		}
	}
}
