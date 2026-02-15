CREATE TABLE IF NOT EXISTS processed_events (
  event_id TEXT PRIMARY KEY,
  occurred_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS active_trips (
  trip_id TEXT PRIMARY KEY,
  carrier_id TEXT NOT NULL,
  origin_lat DOUBLE PRECISION NOT NULL,
  origin_lng DOUBLE PRECISION NOT NULL,
  destination_lat DOUBLE PRECISION NOT NULL,
  destination_lng DOUBLE PRECISION NOT NULL,
  assigned_at TIMESTAMPTZ NOT NULL,
  estimated_duration INTERVAL NOT NULL,
  status TEXT NOT NULL DEFAULT 'assigned'
);

CREATE TABLE IF NOT EXISTS carrier_locations (
  trip_id TEXT NOT NULL REFERENCES active_trips(trip_id),
  lat DOUBLE PRECISION NOT NULL,
  lng DOUBLE PRECISION NOT NULL,
  recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deviation_meters DOUBLE PRECISION,
  estimated_arrival TIMESTAMPTZ,
  PRIMARY KEY (trip_id, recorded_at)
);

CREATE TABLE IF NOT EXISTS active_alerts (
  alert_id UUID PRIMARY KEY,
  alert_type TEXT NOT NULL,
  trip_id TEXT NOT NULL,
  carrier_id TEXT NOT NULL,
  message TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  resolved_at TIMESTAMPTZ,
  severity TEXT NOT NULL DEFAULT 'warning'
);

CREATE TABLE IF NOT EXISTS websocket_sessions (
  session_id TEXT PRIMARY KEY,
  operator_id TEXT,
  connected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_heartbeat TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS outbox_events (
  id UUID PRIMARY KEY,
  event_type TEXT NOT NULL,
  correlation_id TEXT NOT NULL,
  topic TEXT NOT NULL,
  partition_key TEXT NOT NULL,
  payload JSONB NOT NULL,
  occurred_at TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  attempts INT NOT NULL DEFAULT 0,
  last_error TEXT,
  next_attempt_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
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

ALTER TABLE active_trips
  ADD COLUMN IF NOT EXISTS total_route_distance_meters DOUBLE PRECISION;
