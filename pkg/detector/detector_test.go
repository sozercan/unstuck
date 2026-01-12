package detector

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		d        time.Duration
		expected string
	}{
		{"hours", 2 * time.Hour, "2h"},
		{"minutes", 45 * time.Minute, "45m"},
		{"seconds", 30 * time.Second, "30s"},
		{"days", 72 * time.Hour, "3d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.d)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainsVerb(t *testing.T) {
	tests := []struct {
		name     string
		verbs    []string
		verb     string
		expected bool
	}{
		{"contains delete", []string{"get", "list", "delete"}, "delete", true},
		{"missing delete", []string{"get", "list"}, "delete", false},
		{"empty verbs", []string{}, "delete", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsVerb(tt.verbs, tt.verb)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		val      string
		expected bool
	}{
		{"exact match", []string{"Hello", "World"}, "World", true},
		{"case insensitive", []string{"hello", "world"}, "WORLD", true},
		{"no match", []string{"Hello", "World"}, "foo", false},
		{"empty slice", []string{}, "foo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsIgnoreCase(tt.slice, tt.val)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseDiscoveryFailureMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected []string
	}{
		{
			name:     "no groups failed",
			message:  "some other error",
			expected: nil,
		},
		{
			name:     "single group failed",
			message:  "unable to retrieve the complete list of server APIs: metrics.k8s.io/v1beta1: the server is currently unable to handle the request",
			expected: []string{"metrics.k8s.io/v1beta1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDiscoveryFailureMessage(tt.message)
			assert.Equal(t, tt.expected, result)
		})
	}
}
