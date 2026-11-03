// Companion to test-native-deploy.sh; no credentials are printed.
import {readFileSync,writeFileSync} from 'node:fs';
const state=process.env.SSHGUARD_TEST_STATE;
if(process.env.GITHUB_ACTIONS!=='true' || !state) throw Error('Disposable CI only');
const origin=process.env.SSHGUARD_TEST_ORIGIN || 'https://guard.sshguard.test';
const password=process.env.SSHGUARD_TEST_PASSWORD || JSON.parse(readFileSync('/opt/SSHGuard/Server/api.env','utf8').split('\n').find(x=>x.startsWith('ADMIN_PASSWORD=')).slice('ADMIN_PASSWORD='.length));
let cookie='';
async function api(path,method='GET',body){
 const res=await fetch(origin+'/api/v1'+path,{method,headers:{Origin:origin,Cookie:cookie,'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});
 if(!res.ok) throw Error(`Request ${path} failed: ${res.status}`);
 if(res.headers.get('set-cookie')) cookie=res.headers.get('set-cookie').split(';')[0];
 return (await res.json()).data;
}
await api('/auth/login','POST',{username:'admin',password});
if(process.argv[2]==='enroll'){
 const {id}=await api('/servers','POST',{name:process.env.SSHGUARD_TEST_SERVER_NAME || 'CI-native-systemd'});
 const result=await api(`/servers/${id}/enrollment`,'POST',{});
 const token=result.commands.ubuntu.match(/'([a-f0-9]{64})'/)?.[1];
 if(!token) throw Error('Generated installation command missing token');
 writeFileSync(`${state}/enrollment`,token,{mode:0o600});
 writeFileSync(`${state}/server`,id,{mode:0o600});
}else if(process.argv[2]==='queue-update'){
 const cfg=JSON.parse(readFileSync('/opt/SSHGuard/Agent/agent.json','utf8'));
 const agent=async(path,body)=>{
  const res=await fetch(origin+'/api/v1/agent/'+path,{method:'POST',headers:{Authorization:'Bearer '+cfg.token,'Content-Type':'application/json'},body:JSON.stringify(body)});
  if(!res.ok)throw Error('Agent fixture request failed: '+res.status);
 };
 await agent('update/poll',{});
 await agent('heartbeat',{version:'0.3.21-test'});
 await api(`/servers/${cfg.server_id}/update`,'POST',{});
}else if(process.argv[2]==='verify-update'){
 const id=readFileSync(`${state}/server`,'utf8');
 const expected=readFileSync('AGENT_VERSION','utf8').trim();
 let confirmed=false;
 for(let i=0;i<40;i++){
  const server=await api('/servers/'+id);
  if(server.update_status==='failed')throw Error('Platform-triggered update failed');
  if(server.update_status==='success' && server.agent_version===expected){confirmed=true;break;}
  // An idempotent update leaves the collector running; its next periodic
  // heartbeat replaces the old-version fixture used to request this task.
  await new Promise(resolve=>setTimeout(resolve,1000));
 }
 if(!confirmed)throw Error('Platform-triggered update or version confirmation timed out');
}else{
 const server=await api('/servers/'+readFileSync(`${state}/server`,'utf8'));
 if(server.status!=='online' || server.agent_version!==readFileSync('AGENT_VERSION','utf8').trim()) throw Error('Agent status/version incorrect');
 let collected=false;
 for(let i=0;i<15;i++){
   const result=await api('/events?server_id='+server.id+'&username=ci-fixture');
   if(result.items.some(x=>x.outcome==='success') && result.items.some(x=>x.outcome==='failure')){collected=true;break;}
   await new Promise(resolve=>setTimeout(resolve,1000));
 }
 if(!collected)throw Error('Installed Agent failed to collect and upload real syslog/journal fixture events');
 const update=await api('/agent/update-commands');
 if(!update.commands.windows.includes('-Update')) throw Error('Update command missing');
 const result=await api('/dashboard/summary');
 if(result.servers!==(process.argv[2]==='verify-alpine'?2:1)) throw Error('Upgrade lost or duplicated server');
}
await api('/auth/logout','POST',{});
