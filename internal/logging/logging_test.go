package logging

import "testing"

func TestNormalizeLevel(t *testing.T) {
	tests := map[string]string{
		"":        "info",
		"INFO":    "info",
		" debug ": "debug",
		"warn":    "warn",
		"warning": "warn",
		"error":   "error",
	}
	for input, want := range tests {
		got, err := NormalizeLevel(input)
		if err != nil {
			t.Fatalf("NormalizeLevel(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeLevel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeLevelRejectsUnknownLevel(t *testing.T) {
	if _, err := NormalizeLevel("trace"); err == nil {
		t.Fatal("expected unknown log level error")
	}
}

func TestSetLevelRejectsUnknownLevel(t *testing.T) {
	if err := SetLevel("trace"); err == nil {
		t.Fatal("expected unknown log level error")
	}
}
