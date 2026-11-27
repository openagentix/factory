package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/floor"
)

var oxfBinary string

// TestMain builds the oxf binary once. The stub harness is a subcommand of it.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "demo-oxf-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	oxfBinary = filepath.Join(dir, "oxf")
	cmd := exec.Command("go", "build", "-o", oxfBinary, "github.com/openagentix/factory/cmd/oxf")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building oxf: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func testConfig(t *testing.T) Config {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return Config{DataDir: dir, Program: oxfBinary}
}

func newDemo(t *testing.T) *Demo {
	t.Helper()
	d, err := New(context.Background(), testConfig(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// tfloor is the snapshot as a test reads it. Calls carry a fact map that has
// no decoder, so only their id and kind are read.
type tfloor struct {
	Mode     floor.Mode                            `json:"mode"`
	Demo     *floor.Demo                           `json:"demo"`
	Sources  map[floor.SourceKey]floor.SourceState `json:"sources"`
	Signal   floor.Signal                          `json:"signal"`
	Stations []floor.Station                       `json:"stations"`
	Pieces   []floor.Piece                         `json:"pieces"`
	Goals    []floor.Goal                          `json:"goals"`
	Calls    []struct {
		ID   string         `json:"id"`
		Kind floor.CallKind `json:"kind"`
	} `json:"calls"`
	Log        []floor.JournalEntry `json:"log"`
	JournalSeq int64                `json:"journalSeq"`
}

func snap(t testing.TB, d *Demo) tfloor {
	t.Helper()
	b, err := d.Floor(context.Background())
	if err != nil {
		t.Fatalf("Floor: %v", err)
	}
	var f tfloor
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("snapshot does not parse: %v", err)
	}
	return f
}

// invariants are what every snapshot of the story must satisfy.
func invariants(f tfloor) error {
	if f.Mode != floor.ModeDemo || f.Demo == nil || f.Demo.Note != "made_up_project_fake_host" {
		return fmt.Errorf("not marked as demo: mode=%q demo=%v", f.Mode, f.Demo)
	}
	for _, k := range []floor.SourceKey{floor.SourceHost, floor.SourceStore, floor.SourceStaging, floor.SourceGate, floor.SourceAcceptance, floor.SourceConfig} {
		if f.Sources[k] != floor.SourceOK {
			return fmt.Errorf("source %s is %s", k, f.Sources[k])
		}
	}
	if f.Signal.Lamp == floor.LampUnknown || len(f.Signal.Inputs.UnknownSources) > 0 {
		return fmt.Errorf("lamp is unknown")
	}
	for _, p := range f.Pieces {
		if p.Reason == nil {
			continue
		}
		switch p.Reason.Code {
		case floor.InvalidFacts, floor.ReadFailed, floor.Unknown:
			return fmt.Errorf("piece %s has reason %s", p.ID, p.Reason.Code)
		}
	}
	if len(f.Stations) != 7 {
		return fmt.Errorf("%d stations", len(f.Stations))
	}
	return nil
}

func piece(f tfloor, id string) *floor.Piece {
	for i := range f.Pieces {
		if f.Pieces[i].ID == id {
			return &f.Pieces[i]
		}
	}
	return nil
}

func code(p *floor.Piece) floor.ReasonCode {
	if p == nil || p.Reason == nil {
		return ""
	}
	return p.Reason.Code
}

func waitDone(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Minute):
		t.Fatal("timed out")
	}
}
