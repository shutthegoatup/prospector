-- The secrets an endpoint's deliveries are signed with, as a key set. Each endpoint's
-- secret, and the one it replaced while that still signs, are carried into it.

-- +goose Up
CREATE TABLE IF NOT EXISTS endpoint_keys (
    id bigserial PRIMARY KEY,
    org_id bigint NOT NULL,
    endpoint_id bigint NOT NULL,
    kid text NOT NULL,
    secret text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    expires_at timestamp with time zone
);
CREATE INDEX IF NOT EXISTS idx_endpoint_keys_org_id ON endpoint_keys USING btree (org_id);
CREATE INDEX IF NOT EXISTS idx_endpoint_keys_endpoint_id ON endpoint_keys USING btree (endpoint_id);

INSERT INTO endpoint_keys (org_id, endpoint_id, kid, secret, created_at, expires_at)
SELECT org_id, id, md5(random()::text), secret, created_at, NULL FROM webhook_endpoints WHERE secret <> '';
INSERT INTO endpoint_keys (org_id, endpoint_id, kid, secret, created_at, expires_at)
SELECT org_id, id, md5(random()::text), previous_secret, created_at, previous_until FROM webhook_endpoints
WHERE previous_secret <> '' AND previous_until > now();

-- +goose Down
DROP TABLE IF EXISTS endpoint_keys;
