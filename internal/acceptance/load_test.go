package acceptance

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestManifestPath(t *testing.T) {
	if got := ManifestPath("acceptance", 40); got != "acceptance/40/manifest.json" {
		t.Errorf("got %q", got)
	}
	if got := ManifestPath("qa/checks", 7); got != "qa/checks/7/manifest.json" {
		t.Errorf("got %q", got)
	}
}

func TestLoadManifest_FromACommitNotAWorkingDirectory(t *testing.T) {
	tr := newTestRepo(t)
	tr.writeManifest(40, crit(1, "A customer can move a booking", "true"))
	first := tr.commit("checks")
	// Later the manifest changes. The first commit must still give the first one.
	tr.writeManifest(40, crit(1, "changed", "true"), crit(2, "second", ""))
	second := tr.commit("later")

	r := tr.open()
	m, err := LoadManifest(context.Background(), r, first, "", 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Criteria) != 1 || m.Criteria[0].Text != "A customer can move a booking" {
		t.Errorf("manifest = %+v", m)
	}
	m, err = LoadManifest(context.Background(), r, second, "", 40)
	if err != nil || len(m.Criteria) != 2 {
		t.Errorf("second = %+v, %v", m, err)
	}
}

func TestLoadManifest_Refusals(t *testing.T) {
	tr := newTestRepo(t)
	tr.write("acceptance/41/manifest.json", `{"schemaVersion":1,"goal":99,"criteria":[{"id":1,"text":"a","run":"true"}]}`)
	tr.write("acceptance/42/manifest.json", `{"schemaVersion":1,"goal":42,"criteria":[],"extra":1}`)
	tr.write("acceptance/43/manifest.json", strings.Repeat(" ", MaxManifestBytes+1))
	var many strings.Builder
	many.WriteString(`{"schemaVersion":1,"goal":44,"criteria":[`)
	for i := 1; i <= MaxCriteria+1; i++ {
		if i > 1 {
			many.WriteString(",")
		}
		fmt.Fprintf(&many, `{"id":%d,"text":"t","run":"true"}`, i)
	}
	many.WriteString("]}")
	tr.write("acceptance/44/manifest.json", many.String())
	tr.write("acceptance/45/manifest.json", fmt.Sprintf(`{"schemaVersion":1,"goal":45,"criteria":[{"id":1,"text":%q,"run":"true"}]}`, strings.Repeat("w", MaxTextBytes+1)))
	tr.write("acceptance/46/manifest.json", fmt.Sprintf(`{"schemaVersion":1,"goal":46,"criteria":[{"id":1,"text":"t","run":%q}]}`, strings.Repeat("r", MaxRunBytes+1)))
	c := tr.commit("x")
	r := tr.open()

	cases := []struct {
		name string
		goal int
		path string
		want string
	}{
		{"goal mismatch", 41, "", "names goal 99"},
		{"unknown field", 42, "", "acceptance manifest"},
		{"too big", 43, "", "over the limit"},
		{"too many criteria", 44, "", "more than"},
		{"long wording", 45, "", "wording"},
		{"long command", 46, "", "command"},
		{"missing", 47, "", "no such file"},
		{"bad goal number", 0, "", "goal"},
		{"bad path", 41, "../x", "acceptance path"},
		{"bad path glob", 41, "a*", "acceptance path"},
	}
	for _, tc := range cases {
		_, err := LoadManifest(context.Background(), r, c, tc.path, tc.goal)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}
