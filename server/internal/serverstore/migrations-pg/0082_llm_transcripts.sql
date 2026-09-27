-- 0082: encrypted, request-scoped LLM transcript retention.
-- The request is stored once and the user-visible response is stored as ordered
-- encrypted chunks so streaming responses never need to be buffered in memory.
CREATE TABLE IF NOT EXISTS llm_transcripts (
  id BIGSERIAL PRIMARY KEY,
  request_id TEXT NOT NULL UNIQUE,
  user_id INTEGER NOT NULL REFERENCES users(id),
  endpoint TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  request_body_enc TEXT NOT NULL,
  status_code INTEGER NOT NULL DEFAULT 0,
  response_bytes BIGINT NOT NULL DEFAULT 0,
  response_sha256 TEXT NOT NULL DEFAULT '',
  audit_status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_llm_transcripts_user_time
  ON llm_transcripts(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_transcripts_model_time
  ON llm_transcripts(model, created_at DESC);

CREATE TABLE IF NOT EXISTS llm_transcript_chunks (
  transcript_id BIGINT NOT NULL REFERENCES llm_transcripts(id) ON DELETE CASCADE,
  seq BIGINT NOT NULL,
  payload_enc TEXT NOT NULL,
  byte_length BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (transcript_id, seq)
);
