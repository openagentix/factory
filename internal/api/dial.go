package api

import "fmt"

// Level is the dial level of one unattended task. It sets where the factory
// stops for a person after the plan is approved.
type Level int

// Dial levels.
const (
	// LevelOff means the task does not run.
	LevelOff Level = 0
	// LevelGuided means a person approves each output before it is combined.
	LevelGuided Level = 1
	// LevelAutopilot means nobody approves anything until Accept.
	LevelAutopilot Level = 2
	// LevelAutomaticAccept means nobody approves anything, for classes of
	// change that earned it. Shown, not active, in the first release.
	LevelAutomaticAccept Level = 3
)

// DefaultLevel is the level a task starts at.
const DefaultLevel = LevelAutopilot

// Valid reports whether l is one of the four levels.
func (l Level) Valid() bool { return l >= LevelOff && l <= LevelAutomaticAccept }

// String is the name of the level.
func (l Level) String() string {
	switch l {
	case LevelOff:
		return "off"
	case LevelGuided:
		return "guided"
	case LevelAutopilot:
		return "autopilot"
	case LevelAutomaticAccept:
		return "automatic accept"
	}
	return "unknown"
}

// CheckLevel returns an error unless level is a real level that is at or
// below the ceiling, and the ceiling is a real level.
func CheckLevel(level, ceiling Level) error {
	if !ceiling.Valid() {
		return fmt.Errorf("ceiling %d is not a dial level (0 to 3)", int(ceiling))
	}
	if !level.Valid() {
		return fmt.Errorf("level %d is not a dial level (0 to 3)", int(level))
	}
	if level > ceiling {
		return fmt.Errorf("level %d (%s) is above the ceiling %d (%s)", int(level), level, int(ceiling), ceiling)
	}
	return nil
}
