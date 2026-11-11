import test from 'node:test';
import assert from 'node:assert/strict';
import { formatAlertMessage, normalizeRule } from '../src/lib/alertPresentation.ts';

test('alert translations preserve identities, diagnostics and unknown values', () => {
 const message='服务器：success\n用户：password\n类型：ssh.login\n来源：8.8.8.8:22\n认证方式：password\n结果：success\n采集状态：error\n诊断：error from service';
 assert.equal(formatAlertMessage(message),'服务器：success\n用户：password\n类型：SSH 登录成功\n来源：8.8.8.8:22\n认证方式：密码\n结果：成功\n采集状态：异常\n诊断：error from service');
 assert.equal(formatAlertMessage('认证方式：custom-auth\n类型：custom.login'),'认证方式：custom-auth\n类型：custom.login');
 assert.equal(formatAlertMessage(null),'');
});
test('online rules discard hidden fields when switching from other rule types', () => {
 const rule={kind:'agent_online',threshold_count:0,window_seconds:0,trusted_cidrs:['invalid'],cooldown_seconds:60,channel_ids:['channel'],server_group:'prod'};
 assert.deepEqual(normalizeRule(rule),{...rule,threshold_count:1,window_seconds:300,trusted_cidrs:[]});
 assert.equal(rule.threshold_count,0);
 assert.deepEqual(normalizeRule({...rule,kind:'failure_threshold'}),{...rule,kind:'failure_threshold'});
});
