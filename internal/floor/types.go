package floor

// Every enum below is a named string type. Its zero value is invalid: a blank
// never means a good state.

type Lamp string

const (
	LampRed     Lamp = "red"
	LampAmber   Lamp = "amber"
	LampGreen   Lamp = "green"
	LampOff     Lamp = "off"
	LampUnknown Lamp = "unknown"
)

func (x Lamp) Valid() bool {
	switch x {
	case LampRed, LampAmber, LampGreen, LampOff, LampUnknown:
		return true
	}
	return false
}

// LampWord is the plain word beside the lamp. The off lamp has two words.
type LampWord string

const (
	WordCouldNotRead LampWord = "could_not_read"
	WordStuck        LampWord = "stuck"
	WordNeedsPerson  LampWord = "needs_a_person"
	WordRunning      LampWord = "running"
	WordPaused       LampWord = "paused"
	WordIdle         LampWord = "idle"
)

func AllLampWords() []LampWord {
	return []LampWord{WordCouldNotRead, WordStuck, WordNeedsPerson, WordRunning, WordPaused, WordIdle}
}

func (x LampWord) Valid() bool {
	for _, w := range AllLampWords() {
		if x == w {
			return true
		}
	}
	return false
}

type Mode string

const (
	ModeDemo Mode = "demo"
	ModeLive Mode = "live"
	// ModeUnknown is shown when the mode could not be read. The Floor never
	// claims demo or live without knowing.
	ModeUnknown Mode = "unknown"
)

func (x Mode) Valid() bool { return x == ModeDemo || x == ModeLive || x == ModeUnknown }

type SourceKey string

const (
	SourceHost       SourceKey = "host"
	SourceStore      SourceKey = "store"
	SourceStaging    SourceKey = "staging"
	SourceGate       SourceKey = "gate"
	SourceAcceptance SourceKey = "acceptance"
	SourceTrajectory SourceKey = "trajectory"
	SourceTeam       SourceKey = "team"
	SourceConfig     SourceKey = "config"
)

// AllSourceKeys is in the order the record lists them.
func AllSourceKeys() []SourceKey {
	return []SourceKey{SourceHost, SourceStore, SourceStaging, SourceGate, SourceAcceptance, SourceTrajectory, SourceTeam, SourceConfig}
}

func (x SourceKey) Valid() bool {
	for _, k := range AllSourceKeys() {
		if x == k {
			return true
		}
	}
	return false
}

type SourceState string

const (
	SourceOK         SourceState = "ok"
	SourceNotBuilt   SourceState = "not_built"
	SourceReadFailed SourceState = "read_failed"
)

func (x SourceState) Valid() bool {
	return x == SourceOK || x == SourceNotBuilt || x == SourceReadFailed
}

// StationNo is 1 to 7. Zero is invalid.
type StationNo int

func (x StationNo) Valid() bool { return x >= 1 && x <= 7 }

type StationKey string

const (
	StationPlan         StationKey = "plan"
	StationWriteChecks  StationKey = "write_checks"
	StationBuild        StationKey = "build"
	StationCheck        StationKey = "check"
	StationSecondOpinon StationKey = "second_opinion"
	StationHoldingArea  StationKey = "holding_area"
	StationAccepted     StationKey = "accepted"
)

func allStationKeys() []StationKey {
	return []StationKey{StationPlan, StationWriteChecks, StationBuild, StationCheck, StationSecondOpinon, StationHoldingArea, StationAccepted}
}

func (x StationKey) Valid() bool {
	for _, k := range allStationKeys() {
		if x == k {
			return true
		}
	}
	return false
}

// Key returns the station's key, or "" for an invalid number.
func (x StationNo) Key() StationKey {
	if !x.Valid() {
		return ""
	}
	return allStationKeys()[x-1]
}

type Who string

const (
	WhoPerson  Who = "person"
	WhoAgent   Who = "agent"
	WhoFactory Who = "factory"
)

func (x Who) Valid() bool { return x == WhoPerson || x == WhoAgent || x == WhoFactory }

type StationSource string

const (
	StationReal       StationSource = "real"
	StationPartlyReal StationSource = "partly_real"
	StationScripted   StationSource = "scripted"
	StationNotBuilt   StationSource = "not_built"
)

func (x StationSource) Valid() bool {
	return x == StationReal || x == StationPartlyReal || x == StationScripted || x == StationNotBuilt
}

// BayState is the state of one station. The worst piece wins, in this order:
// stuck, waiting for a person, working, waiting for a machine, ready, empty.
// Unknown and not_built are for a bay whose source cannot say.
type BayState string

const (
	BayStuck          BayState = "stuck"
	BayWaitingPerson  BayState = "waiting_person"
	BayWorking        BayState = "working"
	BayWaitingMachine BayState = "waiting_machine"
	BayReady          BayState = "ready"
	BayEmpty          BayState = "empty"
	BayUnknown        BayState = "unknown"
	BayNotBuilt       BayState = "not_built"
)

func (x BayState) Valid() bool {
	switch x {
	case BayStuck, BayWaitingPerson, BayWorking, BayWaitingMachine, BayReady, BayEmpty, BayUnknown, BayNotBuilt:
		return true
	}
	return false
}

type GaugeKey string

const (
	GaugeInWork         GaugeKey = "in_work"
	GaugeAcceptedToday  GaugeKey = "accepted_today"
	GaugeRightFirstTime GaugeKey = "right_first_time"
	GaugeGoalToHolding  GaugeKey = "goal_to_holding"
	GaugeAttention      GaugeKey = "attention"
)

func (x GaugeKey) Valid() bool {
	switch x {
	case GaugeInWork, GaugeAcceptedToday, GaugeRightFirstTime, GaugeGoalToHolding, GaugeAttention:
		return true
	}
	return false
}

type GaugeState string

const (
	GaugeOK           GaugeState = "ok"
	GaugeNotEnoughYet GaugeState = "not_enough_yet"
	GaugeNotBuilt     GaugeState = "not_built"
	GaugeReadFailed   GaugeState = "read_failed"
)

func (x GaugeState) Valid() bool {
	return x == GaugeOK || x == GaugeNotEnoughYet || x == GaugeNotBuilt || x == GaugeReadFailed
}

type GaugeWindow string

const (
	WindowNow   GaugeWindow = "now"
	WindowToday GaugeWindow = "today"
	Window7d    GaugeWindow = "7d"
	Window30d   GaugeWindow = "30d"
)

func (x GaugeWindow) Valid() bool {
	return x == WindowNow || x == WindowToday || x == Window7d || x == Window30d
}

type PieceKind string

const (
	KindPlan   PieceKind = "plan"
	KindChecks PieceKind = "checks"
	KindTicket PieceKind = "ticket"
)

func (x PieceKind) Valid() bool { return x == KindPlan || x == KindChecks || x == KindTicket }

type PieceState string

const (
	StateWorking        PieceState = "working"
	StateWaitingMachine PieceState = "waiting_machine"
	StateWaitingPerson  PieceState = "waiting_person"
	StateStuck          PieceState = "stuck"
	StateReady          PieceState = "ready"
)

func (x PieceState) Valid() bool {
	switch x {
	case StateWorking, StateWaitingMachine, StateWaitingPerson, StateStuck, StateReady:
		return true
	}
	return false
}

type Lane string

const (
	LaneQuick    Lane = "quick"
	LaneStandard Lane = "standard"
)

func (x Lane) Valid() bool { return x == LaneQuick || x == LaneStandard }

type Course string

const (
	CourseOn         Course = "on_course"
	CourseDrifting   Course = "drifting"
	CourseOff        Course = "off_course"
	CourseNotStarted Course = "not_started"
)

func (x Course) Valid() bool {
	return x == CourseOn || x == CourseDrifting || x == CourseOff || x == CourseNotStarted
}

type LineState string

const (
	LineWorking         LineState = "working"
	LineNotWorking      LineState = "not_working"
	LineCouldNotConfirm LineState = "could_not_confirm"
)

func (x LineState) Valid() bool {
	return x == LineWorking || x == LineNotWorking || x == LineCouldNotConfirm
}

type CallKind string

const (
	CallQuestion    CallKind = "question"
	CallConsequence CallKind = "consequence"
	CallPlan        CallKind = "plan"
	CallAccept      CallKind = "accept"
	CallStatement   CallKind = "statement"
)

func AllCallKinds() []CallKind {
	return []CallKind{CallQuestion, CallConsequence, CallPlan, CallAccept, CallStatement}
}

func (x CallKind) Valid() bool {
	for _, k := range AllCallKinds() {
		if x == k {
			return true
		}
	}
	return false
}

type CallWho string

const (
	WhoAnyoneTrusted CallWho = "anyone_trusted"
	WhoThisMachine   CallWho = "this_machine"
	WhoADeveloper    CallWho = "a_developer"
)

func (x CallWho) Valid() bool {
	return x == WhoAnyoneTrusted || x == WhoThisMachine || x == WhoADeveloper
}

type ActionName string

const (
	ActionAnswer  ActionName = "answer"
	ActionYes     ActionName = "yes"
	ActionNo      ActionName = "no"
	ActionApprove ActionName = "approve"
	ActionAccept  ActionName = "accept"
	ActionCancel  ActionName = "cancel"
)

func (x ActionName) Valid() bool {
	switch x {
	case ActionAnswer, ActionYes, ActionNo, ActionApprove, ActionAccept, ActionCancel:
		return true
	}
	return false
}

type JournalKind string

// AllJournalKinds is the list in record 0004.
func AllJournalKinds() []JournalKind {
	return []JournalKind{"plan_approved", "checks_written", "entered_holding_area", "check_failed", "taken_out",
		"question_asked", "answered", "consequence_answered", "could_not_confirm", "accepted", "accept_cancelled",
		"undone", "dial_moved", "paused", "resumed"}
}

func (x JournalKind) Valid() bool {
	for _, k := range AllJournalKinds() {
		if x == k {
			return true
		}
	}
	return false
}

type PendingMode string

const (
	PendingMerge     PendingMode = "merge"
	PendingDeveloper PendingMode = "developer"
)

func (x PendingMode) Valid() bool { return x == PendingMerge || x == PendingDeveloper }

// RiskReason is why a change is high risk. It chooses the consequence phrase.
type RiskReason string

const (
	RiskContract   RiskReason = "contract"
	RiskSecurity   RiskReason = "security"
	RiskDependency RiskReason = "dependency"
	RiskTestChange RiskReason = "test_changed"
)

func AllRiskReasons() []RiskReason {
	return []RiskReason{RiskContract, RiskSecurity, RiskDependency, RiskTestChange}
}

func (x RiskReason) Valid() bool {
	for _, k := range AllRiskReasons() {
		if x == k {
			return true
		}
	}
	return false
}

// GateStatus is what the gate says about one change. It is an input only.
type GateStatus string

const (
	GateWaitingReview   GateStatus = "waiting_review"
	GateConsequenceOpen GateStatus = "consequence_open"
	GatePassed          GateStatus = "passed"
	GateVerifyFailed    GateStatus = "verify_failed"
	GateException       GateStatus = "exception"
)

func (x GateStatus) Valid() bool {
	switch x {
	case GateWaitingReview, GateConsequenceOpen, GatePassed, GateVerifyFailed, GateException:
		return true
	}
	return false
}

// ReasonCode is why a piece is where it is. Each code maps to one state.
type ReasonCode string

const (
	// stuck
	CheckFailed           ReasonCode = "check_failed"
	ProtectedPath         ReasonCode = "protected_path"
	AcceptanceRegressed   ReasonCode = "acceptance_regressed"
	ReviewDisagrees       ReasonCode = "review_disagrees"
	PersonSaidNo          ReasonCode = "person_said_no"
	NoChange              ReasonCode = "no_change"
	BadResult             ReasonCode = "bad_result"
	Deadline              ReasonCode = "deadline"
	PullRequestClosed     ReasonCode = "pull_request_closed"
	NoProcess             ReasonCode = "no_process"
	NoReview              ReasonCode = "no_review"
	InvalidFacts          ReasonCode = "invalid_facts"
	SetupNotFinished      ReasonCode = "setup_not_finished"
	AcceptanceUnconfirmed ReasonCode = "acceptance_unconfirmed"
	AttemptsUsedUp        ReasonCode = "attempts_used_up"
	TakenOut              ReasonCode = "taken_out"
	UntrustedTrigger      ReasonCode = "untrusted_trigger"
	TooManyCutOffs        ReasonCode = "too_many_cut_offs"
	TooManyClaimLost      ReasonCode = "too_many_claim_lost"
	ReadFailed            ReasonCode = "read_failed"
	Unknown               ReasonCode = "unknown"
	// waiting_person
	NeedsHuman           ReasonCode = "needs_human"
	QuestionOpen         ReasonCode = "question_open"
	ConsequenceOpen      ReasonCode = "consequence_open"
	PlanWaits            ReasonCode = "plan_waits"
	AcceptPending        ReasonCode = "accept_pending"
	ProjectChecksFailing ReasonCode = "project_checks_failing"
	LevelOff             ReasonCode = "level_off"
	LevelGuided          ReasonCode = "level_guided"
	LevelUnset           ReasonCode = "level_unset"
	GoalNotApproved      ReasonCode = "goal_not_approved"
	Paused               ReasonCode = "paused"
	// waiting_machine
	Queued            ReasonCode = "queued"
	Cap               ReasonCode = "cap"
	Throttle          ReasonCode = "throttle"
	KeyBusy           ReasonCode = "key_busy"
	StagingFrozen     ReasonCode = "staging_frozen"
	MachineBusy       ReasonCode = "machine_busy"
	WaitingDependency ReasonCode = "waiting_dependency"
	WaitingChecks     ReasonCode = "waiting_checks"
	WaitingCombine    ReasonCode = "waiting_combine"
	CutOff            ReasonCode = "cut_off"
	ClaimLost         ReasonCode = "claim_lost"
	ClaimExpired      ReasonCode = "claim_expired"
	// Added in fix round 1.
	Planned       ReasonCode = "planned"        // a ticket that is not ready yet and has no run
	WaitingReview ReasonCode = "waiting_review" // a run succeeded and the second opinion has not come yet
	NotBuilt      ReasonCode = "not_built"      // the step that comes next is not built yet
)

// AllReasonCodes lists every code once. A test compares it with the table in
// record 0004 and fails when a code has no row.
func AllReasonCodes() []ReasonCode {
	return []ReasonCode{
		CheckFailed, ProtectedPath, AcceptanceRegressed, ReviewDisagrees, PersonSaidNo, NoChange, BadResult, Deadline,
		PullRequestClosed, NoProcess, NoReview, InvalidFacts, SetupNotFinished, AcceptanceUnconfirmed, AttemptsUsedUp,
		TakenOut, UntrustedTrigger, TooManyCutOffs, TooManyClaimLost, ReadFailed, Unknown,
		NeedsHuman, QuestionOpen, ConsequenceOpen, PlanWaits, AcceptPending, ProjectChecksFailing, LevelOff,
		LevelGuided, LevelUnset, GoalNotApproved, Paused,
		Queued, Cap, Throttle, KeyBusy, StagingFrozen, MachineBusy, WaitingDependency, WaitingChecks, WaitingCombine,
		CutOff, ClaimLost, ClaimExpired, Planned, WaitingReview, NotBuilt,
	}
}

// State is the one state of the code. An unknown code has no state: it returns
// "", which is not a valid PieceState.
func (c ReasonCode) State() PieceState {
	switch c {
	case CheckFailed, ProtectedPath, AcceptanceRegressed, ReviewDisagrees, PersonSaidNo, NoChange, BadResult, Deadline,
		PullRequestClosed, NoProcess, NoReview, InvalidFacts, SetupNotFinished, AcceptanceUnconfirmed, AttemptsUsedUp,
		TakenOut, UntrustedTrigger, TooManyCutOffs, TooManyClaimLost, ReadFailed, Unknown:
		return StateStuck
	case NeedsHuman, QuestionOpen, ConsequenceOpen, PlanWaits, AcceptPending, ProjectChecksFailing, LevelOff,
		LevelGuided, LevelUnset, GoalNotApproved, Paused:
		return StateWaitingPerson
	case Queued, Cap, Throttle, KeyBusy, StagingFrozen, MachineBusy, WaitingDependency, WaitingChecks, WaitingCombine,
		CutOff, ClaimLost, ClaimExpired, Planned, WaitingReview, NotBuilt:
		return StateWaitingMachine
	}
	return ""
}

// TakesBlockerState says whether a piece with this code may show the state of
// the piece it waits on. State() is the default state. When the blocker is
// stuck or waits for a person, a piece with one of these two codes is shown
// in that state and keeps its own code. For every other code the state is
// fixed.
func (c ReasonCode) TakesBlockerState() bool { return c == WaitingDependency || c == WaitingChecks }

func (c ReasonCode) Valid() bool { return c.State() != "" }

// ---- output types ----

type Floor struct {
	SchemaVersion int                       `json:"schemaVersion"`
	JournalSeq    int64                     `json:"journalSeq"`
	GeneratedAt   string                    `json:"generatedAt"`
	HostTime      *string                   `json:"hostTime"`
	Mode          Mode                      `json:"mode"`
	Demo          *Demo                     `json:"demo"`
	Repository    string                    `json:"repository"`
	Machine       MachineRef                `json:"machine"`
	Sources       map[SourceKey]SourceState `json:"sources"`
	Signal        Signal                    `json:"signal"`
	HoldingArea   *HoldingArea              `json:"holdingArea"`
	Gauges        []Gauge                   `json:"gauges"`
	Stations      []Station                 `json:"stations"`
	Pieces        []Piece                   `json:"pieces"`
	Goals         []Goal                    `json:"goals"`
	Calls         []Call                    `json:"calls"`
	Log           []JournalEntry            `json:"log"`
}

type Demo struct {
	Story string `json:"story"`
	Note  string `json:"note"`
}

type MachineRef struct {
	ID string `json:"id"`
}

type Signal struct {
	Lamp   Lamp         `json:"lamp"`
	Word   LampWord     `json:"word"`
	Inputs SignalInputs `json:"inputs"`
}

// RULING: movingElsewhere and inHoldingArea are null, not a number, when the
// source behind them is not ok. The record types them as numbers; a zero there
// would be an invented value.
type SignalInputs struct {
	Moving               int          `json:"moving"`
	MovingElsewhere      *int         `json:"movingElsewhere"`
	Stuck                int          `json:"stuck"`
	WaitingForPerson     int          `json:"waitingForPerson"`
	InHoldingArea        *int         `json:"inHoldingArea"`
	OldestStuck          *OldestStuck `json:"oldestStuck"`
	Paused               bool         `json:"paused"`
	ProjectChecksFailing bool         `json:"projectChecksFailing"`
	UnknownSources       []SourceKey  `json:"unknownSources"`
}

type OldestStuck struct {
	Piece   string    `json:"piece"`
	Station StationNo `json:"station"`
	Since   string    `json:"since"`
}

type HoldingArea struct {
	StagingSeq    int64          `json:"stagingSeq"`
	Commit        string         `json:"commit"`
	Published     bool           `json:"published"`
	Frozen        bool           `json:"frozen"`
	PendingAccept *PendingAccept `json:"pendingAccept"`
}

type PendingAccept struct {
	Since       *string     `json:"since"`
	Mode        PendingMode `json:"mode"`
	PullRequest int         `json:"pullRequest"`
}

// Gauge values are whole numbers: in_work a count, accepted_today a count,
// right_first_time a percent, goal_to_holding seconds, attention hundredths of
// an action per goal.
type Gauge struct {
	Key      GaugeKey    `json:"key"`
	State    GaugeState  `json:"state"`
	Value    *int64      `json:"value"`
	Samples  *int        `json:"samples"`
	Window   GaugeWindow `json:"window"`
	Series   []int       `json:"series,omitempty"`
	Machines *int        `json:"machines,omitempty"`
}

// Station adds State, Count and OldestStuck to the record's schema, so the bay
// state is computed here and not guessed by the screen.
type Station struct {
	No          StationNo     `json:"no"`
	Key         StationKey    `json:"key"`
	Who         Who           `json:"who"`
	Source      StationSource `json:"source"`
	State       BayState      `json:"state"`
	Count       *int          `json:"count"`
	OldestStuck *string       `json:"oldestStuck"`
}

type Untrusted struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

type Reason struct {
	Code        ReasonCode `json:"code"`
	Count       int        `json:"count,omitempty"` // for too_many_cut_offs and too_many_claim_lost
	Attempt     int        `json:"attempt,omitempty"`
	MaxAttempts int        `json:"maxAttempts,omitempty"`
	Criteria    []int      `json:"criteria,omitempty"`
}

type PieceMachine struct {
	ID   string `json:"id"`
	Mine bool   `json:"mine"`
}

type PieceClaim struct {
	Since   string `json:"since"`
	Expires string `json:"expires"`
}

// RULING: since is null when the time is unknown or lies in the future. The
// record types it as a string.
type Piece struct {
	ID        string        `json:"id"`
	Kind      PieceKind     `json:"kind"`
	Goal      int           `json:"goal"`
	Ticket    *int          `json:"ticket"`
	Title     Untrusted     `json:"title"`
	Station   StationNo     `json:"station"`
	State     PieceState    `json:"state"`
	Reason    *Reason       `json:"reason"`
	Since     *string       `json:"since"`
	BlockedBy *string       `json:"blockedBy"`
	Machine   *PieceMachine `json:"machine"`
	Claim     *PieceClaim   `json:"claim"`
	Run       *string       `json:"run"`
	Call      *string       `json:"call"`
}

type PieceDetail struct {
	Piece       Piece       `json:"piece"`
	Route       []RouteStep `json:"route"`
	Rulings     []Untrusted `json:"rulings"`
	Unconfirmed []Untrusted `json:"unconfirmed"`
}

type RouteStep struct {
	Station StationNo `json:"station"`
	At      *string   `json:"at"`
}

type Goal struct {
	Goal      int        `json:"goal"`
	Title     Untrusted  `json:"title"`
	Lane      *Lane      `json:"lane"`
	Course    *Course    `json:"course"`
	Lines     []Line     `json:"lines"`
	LinesAsOf *LinesAsOf `json:"linesAsOf"`
}

type LinesAsOf struct {
	StagingSeq int64 `json:"stagingSeq"`
}

type Line struct {
	ID    int       `json:"id"`
	Text  Untrusted `json:"text"`
	State LineState `json:"state"`
	Note  *LineNote `json:"note"`
}

type LineNote struct {
	Code   ReasonCode `json:"code"`
	Ticket int        `json:"ticket,omitempty"`
}

type Call struct {
	ID       string          `json:"id"`
	Kind     CallKind        `json:"kind"`
	Who      CallWho         `json:"who"`
	Since    *string         `json:"since"`
	Goal     *int            `json:"goal"`
	Ticket   *int            `json:"ticket"`
	Question *Untrusted      `json:"question"`
	Facts    map[string]Fact `json:"facts"`
	Actions  []ActionName    `json:"actions"`
	Version  string          `json:"version"`
}

type JournalEntry struct {
	JournalSeq int64           `json:"journalSeq"`
	Time       *string         `json:"time"`
	Kind       JournalKind     `json:"kind"`
	Goal       int             `json:"goal,omitempty"`
	Ticket     int             `json:"ticket,omitempty"`
	Facts      map[string]Fact `json:"facts,omitempty"`
}
