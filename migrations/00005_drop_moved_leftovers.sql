-- What AutoMigrate left behind after #58 moved the configurator columns into facts and
-- #82 moved agent_configs into agents; both were emptied by those moves. A database
-- built from the baseline never had them.

-- +goose Up
DROP TABLE IF EXISTS agent_configs;
ALTER TABLE companies DROP COLUMN IF EXISTS configurator_url, DROP COLUMN IF EXISTS platform, DROP COLUMN IF EXISTS built_by;

-- +goose Down
SELECT 1;
