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

INSERT INTO tenants (id, name, tier)
VALUES (1, 'integration', 'free')
ON CONFLICT (id) DO NOTHING;

INSERT INTO projects (id, tenant_id, name)
VALUES (1, 1, 'integration')
ON CONFLICT (id) DO NOTHING;

INSERT INTO api_keys (tenant_id, project_id, key_hash, label, scopes, key_type)
VALUES
    (1, 1, sha256('ggp_integration_publishable_key'::bytea), 'it-publishable', '{}',                 'publishable'),
    (1, 1, sha256('ggs_integration_secret_key'::bytea),      'it-secret',      '{fleet,p2p_relay}', 'secret')
ON CONFLICT (key_hash) DO NOTHING;

INSERT INTO leaderboards (id, tenant_id, project_id, name, sort_order)
VALUES (1, 1, 1, 'integration', 'desc')
ON CONFLICT (id) DO NOTHING;

-- Keep the sequences ahead of the explicit ids above.
SELECT setval('tenants_id_seq',      GREATEST(1, (SELECT max(id) FROM tenants)));
SELECT setval('projects_id_seq',     GREATEST(1, (SELECT max(id) FROM projects)));
SELECT setval('leaderboards_id_seq', GREATEST(1, (SELECT max(id) FROM leaderboards)));
