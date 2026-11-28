-- Where an organisation's events are posted, each as one of its keys may read them.

-- +goose Up
CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id bigserial PRIMARY KEY,
    org_id bigint NOT NULL,
    key_id bigint NOT NULL,
    url text NOT NULL,
    events text DEFAULT ''::text NOT NULL,
    secret text NOT NULL,
    previous_secret text DEFAULT ''::text NOT NULL,
    previous_until timestamp with time zone,
    disabled_at timestamp with time zone,
    disabled_why text DEFAULT ''::text NOT NULL,
    created_by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_webhook_endpoints_org_id ON webhook_endpoints USING btree (org_id);

-- +goose Down
DROP TABLE IF EXISTS webhook_endpoints;
