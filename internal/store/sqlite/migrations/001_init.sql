-- Run records are stored as validated JSON. The other columns exist to find
-- runs fast. No column holds a credential, and none may be added for one.
CREATE TABLE runs (
	id          TEXT PRIMARY KEY,
	task        TEXT NOT NULL,
	ticket      INTEGER NOT NULL,
	goal        INTEGER NOT NULL,
	phase       TEXT NOT NULL,
	created_at  TEXT NOT NULL,
	started_at  TEXT,
	finished_at TEXT,
	-- worked is 1 once the run has started its work stage.
	worked      INTEGER NOT NULL,
	-- cut_off is 1 when a restart ended the run. It uses no attempt.
	cut_off     INTEGER NOT NULL DEFAULT 0,
	body        TEXT NOT NULL
) WITHOUT ROWID;

CREATE INDEX runs_ticket ON runs (ticket, created_at);
CREATE INDEX runs_goal   ON runs (goal, created_at);
CREATE INDEX runs_phase  ON runs (phase, created_at);

-- The current dial level of each task.
CREATE TABLE dial_levels (
	task  TEXT PRIMARY KEY,
	level INTEGER NOT NULL
) WITHOUT ROWID;

-- Every change to a dial level, with the time and the previous level.
CREATE TABLE dial_log (
	seq      INTEGER PRIMARY KEY AUTOINCREMENT,
	task     TEXT NOT NULL,
	at       TEXT NOT NULL,
	previous INTEGER NOT NULL,
	level    INTEGER NOT NULL
);

CREATE INDEX dial_log_task ON dial_log (task, seq);
