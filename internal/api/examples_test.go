package api

import (
	"os"
	"path/filepath"
	"testing"
)

// parseAll parses every file that matches the glob under examples/ with parse.
// It fails when fewer than min files are found, so a file cannot go missing.
func parseAll(t *testing.T, glob string, min int, parse func([]byte) error) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "examples", glob))
	if err != nil || len(files) < min {
		t.Fatalf("%s: want at least %d files, got %d (%v)", glob, min, len(files), err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := parse(data); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

func TestExamplesParse(t *testing.T) {
	t.Run("tasks", func(t *testing.T) {
		parseAll(t, "tasks/*.yaml", 3, func(b []byte) error { _, err := ParseTask(b); return err })
	})
	t.Run("factory", func(t *testing.T) {
		parseAll(t, "factory.yaml", 1, func(b []byte) error { _, err := ParseFactory(b); return err })
	})
	t.Run("result", func(t *testing.T) {
		parseAll(t, "worker-out/result*.json", 1, func(b []byte) error { _, err := ParseResult(b); return err })
	})
	t.Run("issues", func(t *testing.T) {
		parseAll(t, "worker-out/issues*.json", 1, func(b []byte) error { _, err := ParseIssues(b); return err })
	})
	t.Run("review", func(t *testing.T) {
		parseAll(t, "worker-out/review*.json", 1, func(b []byte) error { _, err := ParseReview(b); return err })
	})
	t.Run("acceptance manifest", func(t *testing.T) {
		parseAll(t, "acceptance/*/manifest.json", 1, func(b []byte) error { _, err := ParseManifest(b); return err })
	})
}

// Every file under examples/ must be checked by one of the tests above.
func TestExamplesAllCovered(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	covered := []string{"tasks/*.yaml", "factory.yaml", "worker-out/*.json", "acceptance/*/manifest.json"}
	set := map[string]bool{}
	for _, g := range covered {
		m, _ := filepath.Glob(filepath.Join(root, g))
		for _, f := range m {
			set[f] = true
		}
	}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !set[p] {
			t.Errorf("%s is not checked by any example test", p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
