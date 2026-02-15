CREATE TABLE IF NOT EXISTS processed_events (
  event_id TEXT PRIMARY KEY,
  occurred_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS pending_confirmations (
  trip_id TEXT PRIMARY KEY,
  batch_id TEXT NOT NULL,
  carrier_id TEXT NOT NULL,
  assigned_at TIMESTAMPTZ NOT NULL,
  timeout_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending'
);

CREATE TABLE IF NOT EXISTS outbox_events (
  id UUID PRIMARY KEY,
  event_type TEXT NOT NULL,
  correlation_id TEXT NOT NULL,
  topic TEXT NOT NULL,
  partition_key TEXT NOT NULL,
  payload JSONB NOT NULL,
  headers JSONB,
  occurred_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INT NOT NULL DEFAULT 0,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  next_attempt_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(event_type, correlation_id)
);

CREATE INDEX IF NOT EXISTS idx_outbox_next_attempt ON outbox_events(next_attempt_time);

CREATE TABLE IF NOT EXISTS published_events (
  event_type TEXT NOT NULL,
  correlation_id TEXT NOT NULL,
  occurred_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (event_type, correlation_id)
);

CREATE TABLE IF NOT EXISTS dead_letter_queue (
  id UUID PRIMARY KEY,
  source_id UUID NOT NULL,
  event_type TEXT NOT NULL,
  topic TEXT NOT NULL,
  partition_key TEXT NOT NULL,
  payload JSONB NOT NULL,
  last_error TEXT,
  attempts INT NOT NULL,
  inserted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  process_status TEXT NOT NULL DEFAULT 'pending'
);
