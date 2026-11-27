-- Indexes for the queries that scanned an organisation's rows, or every organisation's:
-- the Research queue by stage, a company's history and what agents did, newest first,
-- and the campaign sender's sweeps. Built concurrently, so writes carry on meanwhile;
-- that can't run inside a transaction.

-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_companies_org_stage ON companies (org_id, stage);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_events_company_at ON events (org_id, company_id, "at");
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_events_org_at ON events (org_id, "at");
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_recipients_status_claimed ON campaign_recipients (status, claimed_at);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_recipients_org_status_sent ON campaign_recipients (org_id, status, sent_at);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_recipients_org_status_sent;
DROP INDEX CONCURRENTLY IF EXISTS idx_recipients_status_claimed;
DROP INDEX CONCURRENTLY IF EXISTS idx_events_org_at;
DROP INDEX CONCURRENTLY IF EXISTS idx_events_company_at;
DROP INDEX CONCURRENTLY IF EXISTS idx_companies_org_stage;
