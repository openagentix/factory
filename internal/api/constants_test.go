package api

import "testing"

func TestConstants(t *testing.T) {
	tests := []struct{ name, got, want string }{
		{"Group", Group, "factory.openagentix.org"},
		{"Version", Version, "v1alpha1"},
		{"APIVersion", APIVersion, "factory.openagentix.org/v1alpha1"},
		{"ProductName", ProductName, "OpenAgentix Software Factory"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}
