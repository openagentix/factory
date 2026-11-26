package acceptance

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/openagentix/factory/internal/api"
)

// Each check gets its own fresh copy. This measures what that costs with 20
// criteria, in a repository with some files. The bound is generous: it only
// catches a collapse, not a slow machine.
func TestRun_TwentyCriteriaOnTheirOwnCopies(t *testing.T) {
	tr := newTestRepo(t)
	for i := 0; i < 300; i++ {
		tr.write(fmt.Sprintf("src/pkg%d/file.go", i), strings.Repeat("package x\n", 50))
	}
	var cs []api.ManifestCriterion
	for i := 1; i <= 20; i++ {
		cs = append(cs, crit(i, fmt.Sprintf("criterion %d", i), fmt.Sprintf("touch mark && test -f src/pkg%d/file.go", i)))
	}
	tr.writeManifest(40, cs...)
	source := tr.commit("checks")
	tr.write("src/new.go", "package x\n")
	judged := tr.commit("change") // a different commit, so the overlay is built too

	start := time.Now()
	rep, err := Run(context.Background(), tr.open(), Request{Judged: judged, Source: source, Goal: 40}, testOptions())
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Results {
		if r.State != api.StateWorking {
			t.Errorf("criterion %d = %s", r.ID, r.State.Name())
		}
	}
	t.Logf("20 criteria, 300 files, each check on its own copy, overlay included: %v", took)
	if took > 2*time.Minute {
		t.Errorf("took %v", took)
	}
}
