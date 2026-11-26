package api

import "testing"

func TestLevel(t *testing.T) {
	tests := []struct {
		level Level
		value int
		name  string
		valid bool
	}{
		{LevelOff, 0, "off", true},
		{LevelGuided, 1, "guided", true},
		{LevelAutopilot, 2, "autopilot", true},
		{LevelAutomaticAccept, 3, "automatic accept", true},
		{Level(-1), -1, "unknown", false},
		{Level(4), 4, "unknown", false},
	}
	for _, tt := range tests {
		if int(tt.level) != tt.value || tt.level.String() != tt.name || tt.level.Valid() != tt.valid {
			t.Errorf("level %d: name %q valid %v", tt.level, tt.level.String(), tt.level.Valid())
		}
	}
	if DefaultLevel != LevelAutopilot {
		t.Errorf("DefaultLevel = %v, want autopilot", DefaultLevel)
	}
}

func TestCheckLevel(t *testing.T) {
	tests := []struct {
		name        string
		level, ceil Level
		wantErr     bool
	}{
		{"below ceiling", LevelGuided, LevelAutopilot, false},
		{"at ceiling", LevelAutopilot, LevelAutopilot, false},
		{"above ceiling", LevelAutomaticAccept, LevelAutopilot, true},
		{"off under ceiling zero", LevelOff, LevelOff, false},
		{"guided over ceiling zero", LevelGuided, LevelOff, true},
		{"level out of range", Level(5), LevelAutomaticAccept, true},
		{"ceiling out of range", LevelOff, Level(9), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckLevel(tt.level, tt.ceil)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckLevel(%v, %v) = %v", tt.level, tt.ceil, err)
			}
		})
	}
}
