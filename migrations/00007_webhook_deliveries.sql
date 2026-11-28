-- Each event on its way to each endpoint that asked for it, until it is taken or given up.

-- +goose Up
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id bigserial PRIMARY KEY,
    org_id bigint NOT NULL,
    endpoint_id bigint NOT NULL,
    event_id bigint NOT NULL,
    type text NOT NULL,
    message_id text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    next_at timestamp with time zone NOT NULL,
    retry_until timestamp with time zone NOT NULL,
    attempts bigint DEFAULT 0 NOT NULL,
    last_code bigint DEFAULT 0 NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    delivered_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_org_id ON webhook_deliveries USING btree (org_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_endpoint_id ON webhook_deliveries USING btree (endpoint_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_deliveries_message_id ON webhook_deliveries USING btree (message_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_due ON webhook_deliveries USING btree (status, next_at);

-- +goose Down
DROP TABLE IF EXISTS webhook_deliveries;
