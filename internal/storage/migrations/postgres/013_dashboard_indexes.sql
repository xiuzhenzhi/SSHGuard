-- Cover time-window statistics and source ranking without reading event rows.
CREATE INDEX IF NOT EXISTS events_dashboard ON ssh_events(occurred_at,outcome,source_ip);
