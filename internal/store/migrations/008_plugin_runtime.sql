CREATE TABLE plugin_runtime (
    instance_id text PRIMARY KEY,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object')
);
