CREATE TABLE evidence_collections (
    id text PRIMARY KEY,
    resource_id text NOT NULL REFERENCES resources(id),
    plugin_id text NOT NULL,
    instance_digest text NOT NULL,
    state text NOT NULL CHECK (state IN ('pending','running','retry_wait','succeeded','degraded','failed')),
    next_run timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    finished_at timestamptz,
    payload jsonb NOT NULL
);
CREATE INDEX evidence_collections_due ON evidence_collections(next_run,id)
    WHERE state IN ('pending','running','retry_wait');
CREATE INDEX evidence_collections_resource ON evidence_collections(resource_id,created_at DESC,id);
