package gate

import (
	"reflect"
	"testing"
)

func TestChooseReviewer(t *testing.T) {
	tests := []struct {
		name      string
		builder   string
		available []string
		want      Reviewer
	}{
		{"the other one", "claude-code", []string{"claude-code", "codex"}, Reviewer{Harness: "codex", Found: true}},
		{"the other one, order reversed", "codex", []string{"claude-code", "codex"}, Reviewer{Harness: "claude-code", Found: true}},
		{"only the builder's own", "claude-code", []string{"claude-code"}, Reviewer{Harness: "claude-code", SingleOpinion: true, Found: true}},
		{"spelling and spaces are ignored", "Claude-Code", []string{" claude-code ", "CLAUDE-CODE"}, Reviewer{Harness: "claude-code", SingleOpinion: true, Found: true}},
		{"builder not installed any more, another is", "claude-code", []string{"codex"}, Reviewer{Harness: "codex", Found: true}},
		{"nothing available", "claude-code", nil, Reviewer{}},
		{"only empty names", "claude-code", []string{"", "  "}, Reviewer{}},
		{"unknown builder is never different", "", []string{"codex"}, Reviewer{Harness: "codex", SingleOpinion: true, Found: true}},
		{"three, takes the first different in name order", "codex", []string{"zed", "claude-code", "codex"}, Reviewer{Harness: "claude-code", Found: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]string(nil), tc.available...)
			if got := ChooseReviewer(tc.builder, tc.available); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			if !reflect.DeepEqual(before, tc.available) {
				t.Fatal("the input slice was changed")
			}
		})
	}
}
