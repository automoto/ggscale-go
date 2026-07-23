-- Integration-test seed: one tenant, one project, two API keys, one
-- leaderboard. Applied by scripts/integration-test.sh via psql after the
-- server is healthy (the server runs migrations at startup, so a healthy
-- server means the schema exists).
--
-- The server authenticates API keys by SHA-256 of the raw bearer token;
-- the raw tokens below must match the defaults in integration_test.go:
--   publishable: ggp_integration_publishable_key
--   secret:      ggs_integration_secret_key
--
-- Idempotent: safe to re-run against a stack that is already seeded.

-- tier is a smallint (0=free .. 3), CHECK 0..3, default 0.
INSERT INTO tenants (id, name, tier)
VALUES (1, 'integration', 0)
ON CONFLICT (id) DO NOTHING;

INSERT INTO projects (id, tenant_id, name)
VALUES (1, 1, 'integration')
ON CONFLICT (id) DO NOTHING;

-- The publishable key carries the player-tier scopes the game client
-- exercises: matchmaker (ticket create/get/cancel) and p2p_relay (TURN
-- credential issuance). Scopes are deny-by-default, so the router 403s
-- these routes without them. The secret key keeps the server-tier scopes.
INSERT INTO api_keys (tenant_id, project_id, key_hash, label, scopes, key_type)
VALUES
    (1, 1, sha256('ggp_integration_publishable_key'::bytea), 'it-publishable', '{matchmaker,p2p_relay}', 'publishable'),
    (1, 1, sha256('ggs_integration_secret_key'::bytea),      'it-secret',      '{fleet,p2p_relay}',      'secret')
ON CONFLICT (key_hash) DO NOTHING;

-- Enable the p2p_relay feature for the project. Relay is deny-by-default
-- (only matchmaker defaults on), so without this grant the relay handler
-- returns 403 even with the scope. Idempotent via NOT EXISTS since the
-- table has no natural unique key. (The seed runs as the bootstrap
-- superuser, which bypasses the table's FORCE ROW LEVEL SECURITY.)
INSERT INTO feature_grants (tenant_id, project_id, feature, enabled)
SELECT 1, 1, 'p2p_relay', true
WHERE NOT EXISTS (
    SELECT 1 FROM feature_grants
    WHERE tenant_id = 1 AND project_id = 1 AND feature = 'p2p_relay'
);

INSERT INTO leaderboards (id, tenant_id, project_id, name, sort_order)
VALUES (1, 1, 1, 'integration', 'desc')
ON CONFLICT (id) DO NOTHING;

-- Keep the sequences ahead of the explicit ids above.
SELECT setval('tenants_id_seq',      GREATEST(1, (SELECT max(id) FROM tenants)));
SELECT setval('projects_id_seq',     GREATEST(1, (SELECT max(id) FROM projects)));
SELECT setval('leaderboards_id_seq', GREATEST(1, (SELECT max(id) FROM leaderboards)));
