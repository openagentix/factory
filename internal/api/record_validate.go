package api

import "fmt"

// ParseEvidence reads an evidence bundle. Unknown fields are an error.
func ParseEvidence(data []byte) (*Evidence, error) {
	var e Evidence
	if err := decodeJSONStrict(data, &e); err != nil {
		return nil, fmt.Errorf("evidence: %w", err)
	}
	if err := e.Validate(); err != nil {
		return nil, fmt.Errorf("evidence: %w", err)
	}
	return &e, nil
}

// ParseRun reads a run record. Unknown fields are an error.
func ParseRun(data []byte) (*Run, error) {
	var r Run
	if err := decodeJSONStrict(data, &r); err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	return &r, nil
}

func checkMode(p *problems, mode string) {
	if mode != ModeUnattended && mode != ModeWheel {
		p.add("mode", "must be %s or %s", ModeUnattended, ModeWheel)
	}
}

func checkTrigger(p *problems, t TriggerRef) {
	if !validTriggerKind(t.Kind) {
		p.add("trigger.kind", "must be one of %v", triggerKinds)
	}
	if blank(t.Ref) {
		p.add("trigger.ref", "must be set")
	}
}

func checkRunLimits(p *problems, l RunLimits) {
	if l.Deadline != "" {
		if err := parsePositiveDuration(l.Deadline); err != nil {
			p.add("limits.deadline", "must be a positive duration such as 30m: %v", err)
		}
	}
	if l.MaxCostUSD < 0 {
		p.add("limits.maxCostUSD", "must be zero or more")
	}
}

func checkMethods(p *problems, method, used string) {
	if !validMethod(method) {
		p.add("method", "must be builtin, plugin:<name>, or a clean relative file path")
	}
	if !validMethod(used) {
		p.add("methodUsed", "must be builtin, plugin:<name>, or a clean relative file path")
	}
}

func checkPathList(p *problems, field string, list []string) {
	for i, f := range list {
		checkFilePath(p, fmt.Sprintf("%s[%d]", field, i), f)
	}
}

// Validate checks the evidence bundle and reports every problem, with field
// paths. The review, risk and gate fields may be empty before the review ends.
func (e *Evidence) Validate() error {
	var p problems
	if e.SchemaVersion != SchemaVersion {
		p.add("schemaVersion", "must be %d", SchemaVersion)
	}
	if blank(e.Run) {
		p.add("run", "must be set")
	}
	if blank(e.Task) {
		p.add("task", "must be set")
	}
	if e.Attempt < 1 {
		p.add("attempt", "must be 1 or more")
	}
	checkMode(&p, e.Mode)
	checkMethods(&p, e.Method, e.MethodUsed)
	checkTrigger(&p, e.Trigger)
	if blank(e.Worker.Name) {
		p.add("worker.name", "must be set")
	}
	for _, c := range []struct {
		name string
		n    int
	}{{"diff.files", e.Diff.Files}, {"diff.added", e.Diff.Added}, {"diff.removed", e.Diff.Removed}} {
		if c.n < 0 {
			p.add(c.name, "must be zero or more")
		}
	}
	checkPathList(&p, "diff.paths", e.Diff.Paths)
	checkClaims(&p, "claims", e.Claims)
	checkRulings(&p, "rulings", e.Rulings)
	for i, d := range e.Denials {
		base := fmt.Sprintf("denials[%d]", i)
		if blank(d.Request) {
			p.add(base+".request", "must be set")
		}
		if blank(d.Reason) {
			p.add(base+".reason", "must be set")
		}
	}
	checkPathList(&p, "protectedPaths.touched", e.ProtectedPaths.Touched)
	if e.ProtectedPaths.OK != (len(e.ProtectedPaths.Touched) == 0) {
		p.add("protectedPaths.ok", "must be true exactly when no protected path was touched")
	}
	checkPathList(&p, "testFilesChanged", e.TestFilesChanged)
	checkPathList(&p, "testFilesAdded", e.TestFilesAdded)
	for i, c := range e.Checks {
		base := fmt.Sprintf("checks[%d]", i)
		if blank(c.Name) {
			p.add(base+".name", "must be set")
		}
		if blank(c.RanBy) {
			p.add(base+".ranBy", "must be set")
		}
		if c.DurationSeconds < 0 {
			p.add(base+".durationSeconds", "must be zero or more")
		}
	}
	seen := map[int]bool{}
	for i, a := range e.Acceptance {
		base := fmt.Sprintf("acceptance[%d]", i)
		switch {
		case a.ID < 1:
			p.add(base+".id", "must be 1 or more")
		case seen[a.ID]:
			p.add(base+".id", "duplicate criterion %d", a.ID)
		}
		seen[a.ID] = true
		if !a.State.Valid() {
			p.add(base+".state", "must be one of %s, %s, %s", StateWorking, StateNotWorking, StateCouldNotConfirm)
		}
		if a.Before != "" && !a.Before.Valid() {
			p.add(base+".before", "must be one of %s, %s, %s", StateWorking, StateNotWorking, StateCouldNotConfirm)
		}
		if (a.State == StateWorking || a.State == StateNotWorking) && blank(a.Check) {
			p.add(base+".check", "must name the check that gave state %s", a.State)
		}
	}
	if rv := e.Review; rv != nil {
		if blank(rv.Run) {
			p.add("review.run", "must be set")
		}
		if blank(rv.Harness) {
			p.add("review.harness", "must be set")
		}
		switch rv.Verdict {
		case VerdictAgrees, VerdictConcerns, VerdictDisagrees:
		default:
			p.add("review.verdict", "must be one of %s, %s, %s", VerdictAgrees, VerdictConcerns, VerdictDisagrees)
		}
	}
	switch e.Risk.Level {
	case "", RiskLow, RiskMedium, RiskHigh:
	default:
		p.add("risk.level", "must be one of %s, %s, %s", RiskLow, RiskMedium, RiskHigh)
	}
	switch e.Gate {
	case "", GatePending, GatePassed, GateFailed, GateException:
	default:
		p.add("gate", "must be one of %s, %s, %s, %s", GatePending, GatePassed, GateFailed, GateException)
	}
	checkUsage(&p, "usage", e.Usage)
	checkRunLimits(&p, e.Limits)
	return p.err()
}

// Validate checks the run record and reports every problem, with field paths.
func (r *Run) Validate() error {
	var p problems
	if blank(r.ID) {
		p.add("id", "must be set")
	}
	if blank(r.Task) {
		p.add("task", "must be set")
	}
	if r.Attempt < 1 {
		p.add("attempt", "must be 1 or more")
	}
	if r.Goal < 0 {
		p.add("goal", "must be zero or more")
	}
	if r.Ticket < 0 {
		p.add("ticket", "must be zero or more")
	}
	checkMode(&p, r.Mode)
	checkMethods(&p, r.Method, r.MethodUsed)
	checkTrigger(&p, r.Trigger)
	checkRunLimits(&p, r.Limits)
	if !r.Phase.Valid() {
		p.add("phase", "must be one of the run phases")
	}
	if r.CreatedAt.IsZero() {
		p.add("createdAt", "must be set")
	}
	if r.StartedAt != nil && r.FinishedAt != nil && r.FinishedAt.Before(*r.StartedAt) {
		p.add("finishedAt", "must not be before startedAt")
	}
	if r.Input.Task != nil {
		p.addNested("input.task", r.Input.Task.Validate())
	}
	if r.Result != nil {
		p.addNested("result", r.Result.Validate())
	}
	if r.Usage != nil {
		checkUsage(&p, "usage", *r.Usage)
	}
	if r.Provenance != nil {
		p.addNested("provenance", r.Provenance.Validate())
	}
	return p.err()
}
