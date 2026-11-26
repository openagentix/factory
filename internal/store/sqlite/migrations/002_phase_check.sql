-- The phase column repeats the phase in the body. Let the database refuse a
-- value that is not a run phase. SQLite cannot add a CHECK to a table, so the
-- table is made again and the rows are copied.
CREATE TABLE runs_new (
	id          TEXT PRIMARY KEY,
	task        TEXT NOT NULL,
	ticket      INTEGER NOT NULL,
	goal        INTEGER NOT NULL,
	phase       TEXT NOT NULL CHECK (phase IN (
		'queued', 'held', 'working', 'verifying', 'publishing',
		'succeeded', 'failed', 'needs_human', 'cancelled')),
	created_at  TEXT NOT NULL,
	started_at  TEXT,
	finished_at TEXT,
	worked      INTEGER NOT NULL,
	cut_off     INTEGER NOT NULL DEFAULT 0,
	body        TEXT NOT NULL
) WITHOUT ROWID;

INSERT INTO runs_new SELECT id, task, ticket, goal, phase, created_at, started_at, finished_at, worked, cut_off, body FROM runs;
DROP TABLE runs;
ALTER TABLE runs_new RENAME TO runs;

CREATE INDEX runs_ticket ON runs (ticket, created_at);
CREATE INDEX runs_goal   ON runs (goal, created_at);
CREATE INDEX runs_phase  ON runs (phase, created_at);
