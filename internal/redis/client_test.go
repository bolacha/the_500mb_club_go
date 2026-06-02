package redis

import (
	"testing"
)

func TestItoa(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{-1, "-1"},
		{100, "100"},
		{999999999, "999999999"},
	}
	for _, tt := range tests {
		got := itoa(tt.n)
		if got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestItoaInt64(t *testing.T) {
	got := itoa(int64(42))
	if got != "42" {
		t.Errorf("itoa(int64(42)) = %q, want %q", got, "42")
	}
}
