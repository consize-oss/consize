CREATE TABLE discovery_runs (
 id text PRIMARY KEY,
 plugin_id text NOT NULL,
 scope_key text NOT NULL,
 state text NOT NULL CHECK (state IN ('pending','running','retry_wait','succeeded','failed')),
 next_run timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 payload jsonb NOT NULL
);
CREATE UNIQUE INDEX discovery_scope_pending ON discovery_runs(scope_key)
 WHERE state IN ('pending','running','retry_wait');
CREATE INDEX discovery_runs_due ON discovery_runs(next_run,id)
 WHERE state IN ('pending','running','retry_wait');
CREATE INDEX discovery_runs_recent ON discovery_runs(created_at DESC,id);
