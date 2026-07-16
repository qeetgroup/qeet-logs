package notify

import (
	"context"
	"errors"
	"testing"
)

func TestResolveLocale(t *testing.T) {
	cases := []struct {
		name          string
		tenantDefault string
		recipient     string
		want          string
	}{
		{"recipient wins", "en", "ta", "ta"},
		{"recipient region-stripped", "en", "hi-IN", "hi"},
		{"fallback to tenant default", "bn", "", "bn"},
		{"unsupported recipient falls to tenant", "kn", "xx", "kn"},
		{"both unsupported → platform default", "zz", "xx", "en"},
		{"empty → platform default", "", "", "en"},
		{"case-insensitive", "EN", "TA", "ta"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveLocale(c.tenantDefault, c.recipient); got != c.want {
				t.Errorf("ResolveLocale(%q,%q) = %q, want %q", c.tenantDefault, c.recipient, got, c.want)
			}
		})
	}
}

func TestIsSupported(t *testing.T) {
	if !IsSupported("hi") || !IsSupported("ml-IN") {
		t.Error("hi and ml-IN must be supported")
	}
	if IsSupported("fr") {
		t.Error("fr is not in the India-first catalogue")
	}
}

func TestTriggerNotConfigured(t *testing.T) {
	// No URL/key → no network, explicit ErrNotConfigured (never a fake success).
	c := New("", "")
	if c.Configured() {
		t.Fatal("empty client must report not configured")
	}
	err := c.Trigger(context.Background(), "alert-fired", "user@x", "hi", nil)
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}
