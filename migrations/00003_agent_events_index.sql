-- What agents did, newest first: the Agents page counts and pages these, among every
-- other event an organisation has. Only agents' events are indexed, in the order the
-- page reads them, so the count and each page read the index alone.

-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_events_agents ON events (org_id, "at" DESC, id DESC) WHERE "user" LIKE 'agent:%';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_events_agents;
