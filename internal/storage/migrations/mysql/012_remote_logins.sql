-- Event type constraints are replaced by the version-gated migration helper.
SELECT 1;
UPDATE alert_rules SET name='登录成功' WHERE id='00000000-0000-4000-8000-000000000001' AND name='SSH 登录成功';
UPDATE alert_rules SET name='认证失败过多' WHERE id='00000000-0000-4000-8000-000000000002' AND name='SSH 认证失败过多';
