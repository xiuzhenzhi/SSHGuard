// Isolated real-process smoke test. Does not install services or change host settings.
// Usage: node scripts/tests/test-direct-flow.mjs PATH_TO_API PATH_TO_AGENT
import assert from 'node:assert/strict';
import {randomBytes, randomUUID} from 'node:crypto';
import {spawn, execFileSync} from 'node:child_process';
import {mkdtempSync, readFileSync, rmSync, existsSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve, sep} from 'node:path';
import {createServer} from 'node:net';

const [apiPath, agentPath] = process.argv.slice(2).map(path => resolve(path));
assert(apiPath && agentPath, 'Pass API and Agent executable paths');
const root = mkdtempSync(join(tmpdir(), 'sshguard-direct-flow-'));
assert(root.startsWith(resolve(tmpdir())+sep+'sshguard-direct-flow-'), 'Unexpected test directory');
const listener = createServer();
await new Promise(resolve => listener.listen(0, '127.0.0.1', resolve));
const port = listener.address().port;
await new Promise(resolve => listener.close(resolve));
const origin = 'http://127.0.0.1:'+port;
const token = randomBytes(32).toString('hex');
const password = randomBytes(20).toString('hex');
const reportInterval = Number(process.env.SSHGUARD_TEST_REPORT_INTERVAL || 5);
assert(Number.isInteger(reportInterval) && reportInterval >= 1 && reportInterval <= 600);
const env = {...process.env, DB_DRIVER:'sqlite', DATABASE_URL:'file:'+join(root,'sshguard.db').replaceAll('\\','/'),
  PUBLIC_URL:'', ALLOW_INSECURE_HTTP:'true', TRUSTED_PROXY_CIDRS:'none', TLS_CERT_FILE:'', TLS_KEY_FILE:'', WEB_SETUP:'true',
  SETUP_TOKEN:token, SETUP_CONFIG_FILE:join(root,'setup.json'), ENCRYPTION_KEY:randomBytes(32).toString('hex'),
  LISTEN_ADDR:'127.0.0.1:'+port, WEB_DIR:resolve('web/dist'), GIN_MODE:'release'};
let child, cookie = '';
async function start() {
  child = spawn(apiPath, [], {env, stdio:'ignore', windowsHide:true});
  child.on('error', error => { console.error(error.message); });
  for (let i=0;i<100;i++) {
    try { if ((await fetch(origin+'/api/v1/health')).ok) return; } catch {}
    if (child.exitCode !== null) throw Error('API exited during startup');
    await new Promise(resolve => setTimeout(resolve,100));
  }
  throw Error('API did not start');
}
async function stop() {
  if (child && child.exitCode === null) {
    const stopped = new Promise(resolve => child.once('exit',resolve));
    child.kill(); await stopped;
  }
}
async function api(path, method='GET', body, status=200, headers={}) {
  const response = await fetch(origin+'/api/v1'+path, {method, headers:{Origin:origin, Cookie:cookie,
    'Content-Type':'application/json', ...headers}, body:body === undefined ? undefined : JSON.stringify(body)});
  assert.equal(response.status,status,path);
  const setCookie = response.headers.get('set-cookie');
  if (setCookie) { assert(!/;\s*Secure(?:;|$)/i.test(setCookie),'HTTP cookie incorrectly marked Secure'); cookie=setCookie.split(';')[0]; }
  const result = await response.json();
  return result.data;
}
try {
  await start();
  assert.equal((await api('/setup/status')).required,true);
  await api('/setup/complete','POST',{username:'admin',password},403);
  await api('/setup/test','POST',{},403,{'X-Setup-Token':token,Origin:'http://attacker.example'});
  await api('/setup/test','POST',{},200,{'X-Setup-Token':token});
  await api('/setup/complete','POST',{username:'admin',password},200,{'X-Setup-Token':token});
  for (let i=0;i<100;i++) {
    await new Promise(resolve => setTimeout(resolve,100));
    try { if ((await api('/health')).status === 'ok') break; } catch {}
  }
  assert.equal((await api('/health')).status,'ok');
  assert.equal((await api('/setup/status')).required,false);
  await api('/auth/login','POST',{username:'admin',password},403,{Origin:'http://attacker.example'});
  await api('/auth/login','POST',{username:'admin',password});
  const {id} = await api('/servers','POST',{name:'Direct IP smoke fixture'});
  const registration = await api('/servers/'+id+'/enrollment','POST',{report_interval_seconds:reportInterval},200,{'X-Forwarded-Host':'attacker.example','X-Forwarded-Proto':'https'});
  assert.equal(registration.endpoint,origin);
  assert.equal(registration.insecure_http,true);
  for (const [platform,command] of Object.entries(registration.commands)) {
    assert(command.includes(origin));
    assert(command.includes(platform === 'windows' ? '-AllowInsecureHttp' : '--allow-insecure-http'));
    assert(!command.includes('attacker.example'));
    assert(command.includes('https://github.com/xiuzhenzhi/SSHGuard/releases/download/'));
    assert(command.includes("--proto '=https'"));
    assert(!command.includes(origin+'/downloads/'));
  }
  const enrollment = registration.commands.ubuntu.match(/'([a-f0-9]{64})'/)?.[1];
  assert(enrollment,'Generated enrollment token missing');
  const config = join(root,'agent.json');
  const args = ['-enroll','-endpoint',origin,'-config',config];
  assert.throws(() => execFileSync(agentPath,args,{input:enrollment,stdio:['pipe','ignore','pipe'],windowsHide:true}));
  execFileSync(agentPath,[...args,'-allow-insecure-http'],{input:enrollment,stdio:['pipe','ignore','pipe'],windowsHide:true});
  const credential = JSON.parse(readFileSync(config,'utf8'));
  assert.equal(credential.allow_insecure_http,true);
  assert.equal(credential.report_interval_seconds,reportInterval);
  execFileSync(agentPath,['-check','-endpoint',origin,'-config',config],{stdio:'pipe',windowsHide:true});
  await api('/agent/enroll','POST',{token:enrollment,hostname:'replay',distribution:'fixture',version:'0.0.0'},401);
  const agentHeaders = {Authorization:'Bearer '+credential.token};
  await api('/agent/heartbeat','POST',{version:'0.0.0',collector_state:'healthy'},200,agentHeaders);
  const event = {event_id:randomUUID(),event_type:'ssh.login',username:'fixture-user',source_ip:'192.0.2.10',
    source_port:2222,auth_method:'publickey',outcome:'success',occurred_at:new Date().toISOString(),raw_message:'Accepted publickey for fixture-user from 192.0.2.10 port 2222 ssh2'};
  const batch = await api('/agent/events/batch','POST',{events:[event]},200,agentHeaders);
  assert.equal(batch.accepted,1);
  const events = await api('/events?server_id='+id);
  assert(events.items.some(item=>item.username==='fixture-user'),'HTTP event not persisted');
  const updates = await api('/agent/update-commands');
  assert.equal(updates.endpoint,origin);
  assert(updates.commands.windows.includes('-Update -AllowInsecureHttp'));
  await api('/agent/update/poll','POST',{},200,agentHeaders);
  await api('/servers/'+id+'/update','POST',{},409);
  await api('/agent/heartbeat','POST',{version:'0.3.21-test',collector_state:'healthy'},200,agentHeaders);
  await api('/servers/'+id+'/update','POST',{});
  assert((await api('/agent/update/poll','POST',{},200,agentHeaders)).update.id);
  const manifest = readFileSync(env.SETUP_CONFIG_FILE,'utf8');
  await stop(); await start();
  assert.equal((await api('/setup/status')).required,false);
  await api('/auth/login','POST',{username:'admin',password});
  assert.equal((await api('/servers/'+id)).status,'online');
  assert.equal(readFileSync(env.SETUP_CONFIG_FILE,'utf8'),manifest);
  assert(existsSync(join(root,'sshguard.db')));
  console.log('Direct IP flow passed: default SQLite, setup authorization, login, real Agent opt-in/enrollment/heartbeat, events, updates, restart persistence.');
} finally {
  await stop();
  rmSync(root,{recursive:true,force:true});
}
