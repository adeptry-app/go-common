package config

import (
	"slices"
	"strings"
	"testing"
)

// =============================================================================
// GetEnvBool Tests
// =============================================================================

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		set          bool
		defaultValue bool
		expected     bool
	}{
		{"unset returns default true", "", false, true, true},
		{"unset returns default false", "", false, false, false},
		{"empty returns default", "", true, true, true},
		{"whitespace only returns default", "   ", true, true, true},
		{"lowercase true", "true", true, false, true},
		{"uppercase TRUE", "TRUE", true, false, true},
		{"mixed case True", "True", true, false, true},
		{"one", "1", true, false, true},
		{"surrounding whitespace trimmed", " true ", true, false, true},
		{"lowercase false", "false", true, true, false},
		{"uppercase FALSE", "FALSE", true, true, false},
		{"zero", "0", true, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("TEST_BOOL_VAR", tt.value)
			}

			if got := GetEnvBool("TEST_BOOL_VAR", tt.defaultValue); got != tt.expected {
				t.Errorf("GetEnvBool(%q, %v) = %v, want %v", tt.value, tt.defaultValue, got, tt.expected)
			}
		})
	}
}

func TestGetEnvBool_InvalidValuesPanic(t *testing.T) {
	// strconv.ParseBool's single-letter t/f forms are deliberately rejected.
	for _, value := range []string{"yes", "no", "on", "off", "t", "f", "2"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TEST_BOOL_VAR", value)

			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("GetEnvBool should panic for %q", value)
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, "TEST_BOOL_VAR") {
					t.Errorf("panic = %v, want it to name TEST_BOOL_VAR", r)
				}
			}()

			GetEnvBool("TEST_BOOL_VAR", false)
		})
	}
}

// =============================================================================
// GetEnvRequiredList Tests
// =============================================================================

func TestGetEnvRequiredList(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected []string
	}{
		{"single entry", "public-api", []string{"public-api"}},
		{"multiple entries", "public-api,files-api", []string{"public-api", "files-api"}},
		{"surrounding whitespace trimmed", " public-api , files-api ", []string{"public-api", "files-api"}},
		{"blank entries dropped", "public-api,,files-api,", []string{"public-api", "files-api"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_LIST_VAR", tt.value)

			got := GetEnvRequiredList("TEST_LIST_VAR")
			if !slices.Equal(got, tt.expected) {
				t.Errorf("GetEnvRequiredList(%q) = %v, want %v", tt.value, got, tt.expected)
			}
		})
	}
}

func TestGetEnvRequiredList_EmptyPanics(t *testing.T) {
	// Unset and "no usable entry" both have to fail at boot, not hand back nil.
	for _, value := range []string{"", ",", " , "} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TEST_LIST_VAR", value)

			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("GetEnvRequiredList should panic for %q", value)
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, "TEST_LIST_VAR") {
					t.Errorf("panic = %v, want it to name TEST_LIST_VAR", r)
				}
			}()

			GetEnvRequiredList("TEST_LIST_VAR")
		})
	}
}
