package demo

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/openagentix/factory/internal/api"
	"github.com/openagentix/factory/internal/floor"
	"github.com/openagentix/factory/internal/githost"
	"github.com/openagentix/factory/internal/harness"
)

// An agent's word decides nothing. The driver uses the outcome types of the
// packages, and the piece shows the honest state.

func TestAgentSuccessWithNoChangeShowsNoChange(t *testing.T) {
	d := newDemo(t)
	ctx := context.Background()
	j, err := d.ticketJob("ask", 0)
	if err != nil {
		t.Fatal(err)
	}
	j.script = harness.StubScript{Result: okResult("All done, nothing to change.")}
	res, err := d.work(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	if res.ok {
		t.Fatal("a run with no change was treated as a change")
	}
	f := snap(t, d)
	p := piece(f, "t"+strconv.Itoa(j.ticket))
	if p == nil || p.State != floor.StateStuck || code(p) != floor.NoChange {
		t.Fatalf("piece after a no-change run: %+v", p)
	}
	if err := invariants(f); err != nil {
		t.Fatal(err)
	}
}

func TestAgentResultThatDoesNotParseFailsTheRun(t *testing.T) {
	for name, script := range map[string]harness.StubScript{
		"no result file":                          {OmitResult: true, Files: []harness.StubFile{{Path: "booking/x.sh", Content: "echo x\n"}}},
		"exit code not zero":                      {ExitCode: 3, Result: okResult("Done."), Files: []harness.StubFile{{Path: "booking/x.sh", Content: "echo x\n"}}},
		"result says success but files are a lie": {Result: &api.Result{SchemaVersion: 1, Status: "succeeded", Summary: "", Harness: api.HarnessInfo{Name: "stub"}}},
	} {
		t.Run(name, func(t *testing.T) {
			d := newDemo(t)
			j, _ := d.ticketJob("ask", 0)
			j.script = script
			res, err := d.work(context.Background(), j)
			if err != nil {
				t.Fatal(err)
			}
			if res.ok {
				t.Fatal("a bad run was treated as success")
			}
			p := piece(snap(t, d), "t"+strconv.Itoa(j.ticket))
			if p == nil || p.State != floor.StateStuck {
				t.Fatalf("piece: %+v", p)
			}
			if c := code(p); c != floor.BadResult && c != floor.NoChange {
				t.Fatalf("code %s", c)
			}
		})
	}
}

// Text of an issue never reaches a command. The check scripts are fixed text.
func TestHostileIssueTextNeverReachesACommand(t *testing.T) {
	d := newDemo(t)
	ctx := context.Background()
	cwd, _ := os.Getwd()
	evil := `x"; touch INJECTED_TITLE; echo "`
	made, err := d.host.CreateSubIssues(ctx, goalMove, []api.IssueProposal{{Title: evil, Body: "$(touch INJECTED_BODY)\n`touch INJECTED_TICK`", Criteria: []int{1}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{githost.LabelPlanned, githost.LabelReady} {
		if err := d.host.AddLabel(ctx, made[0].Number, l); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.host.PostComment(ctx, made[0].Number, "run999", "; touch INJECTED_COMMENT"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Step(ctx); err != nil { // write_checks runs every real check
		t.Fatal(err)
	}
	if err := invariants(snap(t, d)); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{cwd, d.cfg.DataDir, os.TempDir()} {
		_ = filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
			if err == nil && strings.HasPrefix(e.Name(), "INJECTED") {
				t.Errorf("an issue text ran as a command: %s", p)
			}
			return nil
		})
	}
	for _, c := range taskChecks() {
		if strings.Contains(c.Run, "INJECTED") {
			t.Fatal("a check holds issue text")
		}
	}
}

// ---- structure tests ----

func goFiles(t *testing.T, dirs ...string) map[string]*ast.File {
	t.Helper()
	out := map[string]*ast.File{}
	fs := token.NewFileSet()
	for _, dir := range dirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			f, err := parser.ParseFile(fs, p, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			out[p] = f
		}
	}
	return out
}

// forbiddenFloor are the types that only package floor builds, from Inputs.
var forbiddenFloor = map[string]bool{"Piece": true, "Station": true, "Signal": true, "SignalInputs": true, "Gauge": true,
	"Reason": true, "PieceMachine": true, "Goal": true, "Call": true, "Line": true, "HoldingArea": true}

func floorOutputUses(f *ast.File) []string {
	var bad []string
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := s.X.(*ast.Ident); ok && id.Name == "floor" && forbiddenFloor[s.Sel.Name] {
				bad = append(bad, "floor."+s.Sel.Name)
			}
		}
		return true
	})
	return bad
}

func TestDriverAndCollectorNeverBuildFloorOutput(t *testing.T) {
	for p, f := range goFiles(t, ".", "../floor/collect") {
		if bad := floorOutputUses(f); len(bad) > 0 {
			t.Errorf("%s uses %v: the driver may only write facts and floor.Inputs", p, bad)
		}
	}
	// The detector must catch a violation.
	src := "package x\nimport \"github.com/openagentix/factory/internal/floor\"\nvar _ = floor.Piece{State: floor.StateReady}\n"
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(floorOutputUses(f)) == 0 {
		t.Fatal("the detector missed floor.Piece")
	}
}

// Demo mode runs no shell of its own, opens no network connection and reads
// no credential. The only environment variable it reads is PATH.
func TestNoShellNoNetworkNoCredentials(t *testing.T) {
	for p, f := range goFiles(t, ".", "../floor/collect") {
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			switch path {
			case "os/exec", "net", "net/http", "net/url", "syscall", "unsafe":
				t.Errorf("%s imports %s", p, path)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if s, ok := c.Fun.(*ast.SelectorExpr); ok {
				if id, ok := s.X.(*ast.Ident); ok && id.Name == "os" {
					switch s.Sel.Name {
					case "Environ":
						t.Errorf("%s reads the whole environment", p)
					case "Getenv", "LookupEnv":
						if len(c.Args) != 1 {
							return true
						}
						if lit, ok := c.Args[0].(*ast.BasicLit); !ok || lit.Value != `"PATH"` {
							t.Errorf("%s reads an environment variable other than PATH", p)
						}
					}
				}
			}
			return true
		})
	}
}

func TestStubEnvironmentHoldsNoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok-should-not-pass")
	t.Setenv("ANTHROPIC_API_KEY", "tok-should-not-pass")
	d := newDemo(t)
	for _, kv := range d.env {
		if strings.Contains(kv, "tok-should-not-pass") || strings.HasPrefix(kv, "GITHUB") || strings.HasPrefix(kv, "ANTHROPIC") {
			t.Fatalf("the demo environment holds %q", kv)
		}
	}
}
