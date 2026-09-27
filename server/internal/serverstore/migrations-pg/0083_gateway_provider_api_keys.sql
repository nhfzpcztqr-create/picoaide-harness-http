-- Multiple encrypted upstream API keys with independent cooldown state.
CREATE TABLE IF NOT EXISTS gateway_provider_api_keys (
  id BIGSERIAL PRIMARY KEY,
  provider_id BIGINT NOT NULL REFERENCES gateway_providers(id) ON DELETE CASCADE,
  api_key_enc TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  priority INTEGER NOT NULL DEFAULT 0,
  cooldown_until TIMESTAMPTZ,
  failure_count INTEGER NOT NULL DEFAULT 0,
  last_used_at TIMESTAMPTZ,
  last_error_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS gateway_provider_api_keys_provider_idx
  ON gateway_provider_api_keys(provider_id, enabled, priority, id);
CREATE INDEX IF NOT EXISTS gateway_provider_api_keys_cooldown_idx
  ON gateway_provider_api_keys(provider_id, cooldown_until);

INSERT INTO gateway_provider_api_keys (provider_id, api_key_enc)
SELECT id, api_key_enc
FROM gateway_providers p
WHERE p.api_key_enc <> ''
  AND NOT EXISTS (
    SELECT 1 FROM gateway_provider_api_keys k WHERE k.provider_id = p.id
  );
