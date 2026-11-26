package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{"version prints name and version", []string{"version"}, 0, "oxf " + Version, ""},
		{"no args is a usage error", nil, 2, "", "usage"},
		{"unknown command is a usage error", []string{"nope"}, 2, "", "unknown command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(tt.args, &out, &errOut)
			if code != tt.wantCode {
				t.Fatalf("code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("stdout = %q, want to contain %q", out.String(), tt.wantOut)
			}
			if !strings.Contains(errOut.String(), tt.wantErr) {
				t.Errorf("stderr = %q, want to contain %q", errOut.String(), tt.wantErr)
			}
		})
	}
}
