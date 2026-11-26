package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExamplesParse(t *testing.T) {
	root := filepath.Join("..", "..", "examples")

	wantTasks := []string{"implement.yaml", "review.yaml", "fix-ci.yaml"}
	for _, name := range wantTasks {
		t.Run("tasks/"+name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, "tasks", name))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseTask(data); err != nil {
				t.Fatalf("ParseTask: %v", err)
			}
		})
	}

	t.Run("factory.yaml", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(root, "factory.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseFactory(data); err != nil {
			t.Fatalf("ParseFactory: %v", err)
		}
	})

	// Any other task file added later must parse too.
	files, err := filepath.Glob(filepath.Join(root, "tasks", "*.yaml"))
	if err != nil || len(files) < len(wantTasks) {
		t.Fatalf("expected at least %d task examples, got %d (%v)", len(wantTasks), len(files), err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseTask(data); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}
