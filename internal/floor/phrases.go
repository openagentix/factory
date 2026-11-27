package floor

// PhraseKey names one fixed phrase. Keys are "reason.<code>", "journal.<kind>",
// "lamp.<word>", "call.<kind>" (a consequence has "call.consequence.<risk>"),
// "honest.<state>" and "signal.<part>".
type PhraseKey string

// Slots in a phrase are named between braces. A slot is filled with a Fact or
// with a label the web app builds from a piece id. No slot can carry a title,
// a question or any other text that someone wrote.
//
//	piece     label of a piece from its id: t57 is "Ticket 57", p40 is "Plan of goal 40", k40 is "Checks of goal 40"
//	blocker   the same label, for the piece this one waits on
//	goal, ticket, attempt, maxAttempts, count, from, to, moving, elsewhere   numbers
//	machine   a machine id (letters, digits, dot, underscore, hyphen)
func PhraseSlots() []string {
	return []string{"piece", "blocker", "goal", "ticket", "attempt", "maxAttempts", "count", "from", "to", "moving", "elsewhere", "machine"}
}

// Phrases returns the table of fixed phrases. A fresh map is made at each call
// so no caller can change what another caller reads.
func Phrases() map[PhraseKey]string {
	const tries = " Try {attempt} of {maxAttempts}."
	return map[PhraseKey]string{
		// stuck
		"reason.check_failed":           "{piece}: the checks failed." + tries,
		"reason.protected_path":         "{piece}: the change touched a file the factory may not change." + tries,
		"reason.acceptance_regressed":   "{piece}: the change broke a line that was working." + tries,
		"reason.review_disagrees":       "{piece}: the second opinion disagreed." + tries,
		"reason.person_said_no":         "{piece}: you said no." + tries,
		"reason.no_change":              "{piece}: the agent finished and changed nothing." + tries,
		"reason.bad_result":             "{piece}: the agent's result could not be used." + tries,
		"reason.deadline":               "{piece}: the agent ran out of time." + tries,
		"reason.pull_request_closed":    "{piece}: the change was closed before it was combined." + tries,
		"reason.no_process":             "{piece}: the agent stopped without a result." + tries,
		"reason.no_review":              "{piece}: no second opinion was given.",
		"reason.invalid_facts":          "{piece}: the facts did not agree, so the factory stopped.",
		"reason.setup_not_finished":     "{piece}: the project setup is not finished.",
		"reason.acceptance_unconfirmed": "{piece}: the factory could not confirm that the lines still work.",
		"reason.attempts_used_up":       "{piece}: all {maxAttempts} tries are used.",
		"reason.taken_out":              "{piece}: the change was taken out of the holding area.",
		"reason.untrusted_trigger":      "{piece}: it was started by someone who is not trusted.",
		"reason.too_many_cut_offs":      "{piece}: it was cut off {count} times.",
		"reason.too_many_claim_lost":    "{piece}: another machine took it {count} times.",
		"reason.read_failed":            "{piece}: could not read its state.",
		"reason.unknown":                "{piece}: the factory could not say why.",
		// waiting for a person
		"reason.needs_human":            "{piece}: the agent needs a person.",
		"reason.question_open":          "{piece}: the agent asked a question.",
		"reason.consequence_open":       "{piece}: a change needs your yes or no.",
		"reason.plan_waits":             "{piece}: the plan waits for you.",
		"reason.accept_pending":         "{piece}: waiting for the Accept to finish.",
		"reason.project_checks_failing": "The project's own checks are failing. A developer is needed.",
		"reason.level_off":              "{piece}: this kind of work is switched off on this machine.",
		"reason.level_guided":           "{piece}: this kind of work is set to guided, which starts no work yet.",
		"reason.level_unset":            "{piece}: no level is set for this kind of work on this machine.",
		"reason.goal_not_approved":      "{piece}: the goal is not approved.",
		"reason.paused":                 "The line is paused on this machine.",
		// waiting for a machine
		"reason.queued":             "{piece}: waiting for a machine.",
		"reason.cap":                "{piece}: waiting, the limit of work at once is reached.",
		"reason.throttle":           "{piece}: waiting, the factory is slowing down to stay in its limits.",
		"reason.key_busy":           "{piece}: waiting for other work on the same part to finish.",
		"reason.staging_frozen":     "{piece}: waiting, the holding area is frozen for an Accept.",
		"reason.machine_busy":       "{piece}: waiting, this machine is busy with another job.",
		"reason.waiting_dependency": "{piece}: waiting for {blocker}.",
		"reason.waiting_checks":     "{piece}: waiting for {blocker}.",
		"reason.waiting_combine":    "{piece}: passed, waiting to join the holding area.",
		"reason.cut_off":            "{piece}: the factory stopped while it worked. It will start again.",
		"reason.claim_lost":         "{piece}: another machine took it. This machine stopped.",
		"reason.claim_expired":      "{piece}: the claim by {machine} ran out. It waits for a machine.",
		// the shift log
		"journal.plan_approved":        "Goal {goal}: the plan was approved.",
		"journal.checks_written":       "Goal {goal}: the checks were written.",
		"journal.entered_holding_area": "Ticket {ticket}: the change entered the holding area.",
		"journal.check_failed":         "Ticket {ticket}: the checks failed.",
		"journal.taken_out":            "Ticket {ticket}: the change was taken out of the holding area.",
		"journal.question_asked":       "Ticket {ticket}: the agent asked a question.",
		"journal.answered":             "Ticket {ticket}: the question was answered.",
		"journal.consequence_answered": "Ticket {ticket}: the yes or no was given.",
		"journal.could_not_confirm":    "Goal {goal}: the factory could not confirm a line.",
		"journal.accepted":             "Goal {goal}: accepted.",
		"journal.accept_cancelled":     "The Accept was cancelled.",
		"journal.undone":               "Ticket {ticket}: the change was taken back.",
		"journal.dial_moved":           "The level of a kind of work moved from {from} to {to}.",
		"journal.paused":               "The line was paused on this machine.",
		"journal.resumed":              "The line was resumed on this machine.",
		// the lamp
		"lamp.could_not_read": "Could not read",
		"lamp.stuck":          "Stuck",
		"lamp.needs_a_person": "Needs a person",
		"lamp.running":        "Running",
		"lamp.paused":         "Paused",
		"lamp.idle":           "Idle",
		// calls
		"call.question":                 "{piece}: the agent asked a question.",
		"call.consequence.contract":     "This changes how other programs talk to yours. Is that what you want?",
		"call.consequence.security":     "This changes who may do what in your product. Is that what you want?",
		"call.consequence.dependency":   "This changes a part your product is built from. Is that what you want?",
		"call.consequence.test_changed": "This changes or removes a check that already existed. Is that what you want?",
		"call.plan":                     "Goal {goal}: read the lines and approve the plan.",
		"call.accept":                   "{count} changes are in the holding area. Accept them all, or ask a developer to finish this step.",
		"call.statement":                "The project's own checks are failing. A developer is needed.",
		// the three honest states
		"honest.not_built":      "Not built yet",
		"honest.read_failed":    "Could not read",
		"honest.not_enough_yet": "Not enough yet",
		// the signal sentence
		"signal.moving":    "{moving} moving.",
		"signal.elsewhere": "{elsewhere} claimed by another machine.",
	}
}
