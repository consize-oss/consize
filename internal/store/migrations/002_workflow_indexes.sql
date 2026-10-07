CREATE INDEX recommendations_resource_status_idx ON recommendations(resource_id, status);
CREATE INDEX recommendations_expires_at_idx ON recommendations(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX actions_recommendation_idx ON actions(recommendation_id);
CREATE INDEX actions_resource_status_idx ON actions(resource_id, status);
CREATE INDEX action_events_action_created_idx ON action_events(action_id, created_at);
CREATE INDEX action_events_recommendation_created_idx ON action_events(recommendation_id, created_at);
CREATE INDEX jobs_due_idx ON jobs(state, next_run);
CREATE UNIQUE INDEX jobs_one_active_per_resource_idx ON jobs(resource_id)
WHERE state NOT IN ('verified', 'rolled_back', 'cancelled');
