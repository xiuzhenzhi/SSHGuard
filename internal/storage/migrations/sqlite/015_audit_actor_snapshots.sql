CREATE TABLE IF NOT EXISTS audit_actor_snapshots (audit_id INTEGER PRIMARY KEY, username varchar(64) NOT NULL, FOREIGN KEY(audit_id) REFERENCES audit_logs(id) ON DELETE CASCADE);
