CREATE TABLE IF NOT EXISTS managed_user_configs (
  user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  settings_json TEXT NOT NULL DEFAULT '{}',
  revision BIGINT NOT NULL DEFAULT 1,
  updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS managed_skill_policies (
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  skill_name TEXT NOT NULL,
  mode TEXT NOT NULL DEFAULT 'optional',
  version TEXT NOT NULL DEFAULT '',
  revision BIGINT NOT NULL DEFAULT 1,
  updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, skill_name),
  CONSTRAINT managed_skill_policies_mode_check CHECK (mode IN ('required', 'optional', 'blocked'))
);

CREATE TABLE IF NOT EXISTS managed_client_devices (
  user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id TEXT NOT NULL,
  platform TEXT NOT NULL DEFAULT '',
  client_version TEXT NOT NULL DEFAULT '',
  inventory_json TEXT NOT NULL DEFAULT '[]',
  applied_revision BIGINT NOT NULL DEFAULT 0,
  sync_status TEXT NOT NULL DEFAULT 'ok',
  sync_error TEXT NOT NULL DEFAULT '',
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, device_id)
);

CREATE INDEX IF NOT EXISTS managed_client_devices_last_seen_idx
  ON managed_client_devices(user_id, last_seen_at DESC);
