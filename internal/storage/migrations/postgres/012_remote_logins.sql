ALTER TABLE ssh_events DROP CONSTRAINT IF EXISTS ssh_events_event_type_check;
ALTER TABLE ssh_events ADD CONSTRAINT ssh_events_event_type_check CHECK(event_type IN ('ssh.login','ssh.login_failed','rdp.login','rdp.login_failed','vnc.login','vnc.login_failed','console.login','console.login_failed'));
UPDATE alert_rules SET name='登录成功' WHERE id='00000000-0000-4000-8000-000000000001' AND name='SSH 登录成功';
UPDATE alert_rules SET name='认证失败过多' WHERE id='00000000-0000-4000-8000-000000000002' AND name='SSH 认证失败过多';
