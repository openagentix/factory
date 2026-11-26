package githost

import "testing"

func newFakeWorld(t *testing.T) *world {
	clock := newTestClock()
	fx := DemoFixture()
	f := NewFake(fx, clock.Now)
	c1, c2 := fakeCommits()
	return &world{
		host: f, noAccept: f.ProposeOnly(), clock: clock,
		seedComment: f.SeedComment,
		seedIssue: func(title, author, assoc string, labels ...string) int {
			return f.SeedIssue(title, "", author, assoc, labels...)
		},
		branchHead:   f.BranchHead,
		defaultName:  fx.DefaultBranch,
		mergePR:      f.SimulateMerge,
		closePR:      f.SimulateClose,
		status:       f.Status,
		parentOf:     f.Parent,
		setProtected: f.SetProtected,
		forkPR:       f.SimulateFork,
		setPerm:      f.SetPermission,
		editComment:  f.EditComment,
		repoDir:      t.TempDir(), c1: c1, c2: c2,
	}
}

func fakeCommits() (string, string) {
	return "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
}

func TestFakeSuite(t *testing.T) { runSuite(t, newFakeWorld) }
