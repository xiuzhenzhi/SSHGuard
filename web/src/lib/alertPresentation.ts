const alertLabels: Record<string, Record<string, string>> = {
  '类型': {'ssh.login':'SSH 登录成功','ssh.login_failed':'SSH 登录失败','rdp.login':'远程桌面登录成功','rdp.login_failed':'远程桌面登录失败','vnc.login':'VNC 登录成功','vnc.login_failed':'VNC 登录失败','console.login':'控制台登录成功','console.login_failed':'控制台登录失败'},
  '结果': {success:'成功',failure:'失败'},
  '认证方式': {password:'密码',publickey:'公钥','keyboard-interactive':'键盘交互','keyboard-interactive/pam':'键盘交互（PAM）','gssapi-with-mic':'GSSAPI',rdp:'远程桌面',vnc:'VNC',console:'控制台'},
  '采集状态': {healthy:'正常',starting:'启动中',error:'异常',unknown:'未知'},
};
// Translate only known structured fields, never usernames, hostnames or diagnostics.
export function formatAlertMessage(message: unknown): string {
  return String(message ?? '').split('\n').map(line => {
    const match = /^(类型|结果|认证方式|采集状态)[：:]\s*(.*?)\r?$/.exec(line);
    if (!match) return line;
    const [,key,value] = match;
    return `${key}：${alertLabels[key]?.[value] ?? (value || '未记录')}`;
  }).join('\n');
}
export function normalizeRule(rule: Record<string, any>): Record<string, any> {
  return rule.kind === 'agent_online' ? {...rule,threshold_count:1,window_seconds:300,trusted_cidrs:[]} : rule;
}
