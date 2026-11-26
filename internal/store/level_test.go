package store

import (
	"testing"

	"github.com/openagentix/factory/internal/api"
)

func TestClampLevel(t *testing.T) {
	tests := []struct {
		name           string
		level, ceiling api.Level
		want           api.Level
	}{
		{"below the ceiling", api.LevelGuided, api.LevelAutopilot, api.LevelGuided},
		{"at the ceiling", api.LevelAutopilot, api.LevelAutopilot, api.LevelAutopilot},
		{"ceiling was lowered", api.LevelAutopilot, api.LevelGuided, api.LevelGuided},
		{"ceiling is off", api.LevelAutopilot, api.LevelOff, api.LevelOff},
		{"negative ceiling means off", api.LevelGuided, api.Level(-1), api.LevelOff},
		{"negative level means off", api.Level(-2), api.LevelAutopilot, api.LevelOff},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClampLevel(tc.level, tc.ceiling); got != tc.want {
				t.Errorf("ClampLevel(%d, %d) = %d, want %d", tc.level, tc.ceiling, got, tc.want)
			}
		})
	}
}
