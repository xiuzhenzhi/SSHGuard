// Real HTTPS reverse-proxy test, isolated to temporary paths and loopback ports.
// NODE_EXTRA_CA_CERTS=CERT node scripts/tests/test-reverse-proxy.mjs API NGINX CERT KEY
import assert from 'node:assert/strict';
import {randomBytes} from 'node:crypto';
import {spawn, execFileSync} from 'node:child_process';
import {mkdtempSync, readFileSync, writeFileSync, mkdirSync, rmSync, existsSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve, sep, dirname} from 'node:path';
import {createServer} from 'node:net';
import tls from 'node:tls';

assert.equal(process.argv.length,6,'Pass API, Nginx, certificate and key paths');
const [apiPath,nginxPath,cert,key]=process.argv.slice(2).map(path=>resolve(path));
const bundledWeb=join(dirname(apiPath),'web');
const webDir=existsSync(join(bundledWeb,'index.html'))?bundledWeb:resolve('web/dist');
assert(existsSync(join(webDir,'index.html')),'Build the frontend or pass an API from a native release bundle');
const root=mkdtempSync(join(tmpdir(),'sshguard-proxy-'));
assert(root.startsWith(resolve(tmpdir())+sep+'sshguard-proxy-'),'Unexpected test directory');
const posix=path=>path.replaceAll('\\','/');
async function freePort(){const s=createServer();await new Promise(r=>s.listen(0,'127.0.0.1',r));const port=s.address().port;await new Promise(r=>s.close(r));return port;}
const upstream=await freePort(),port=await freePort(),origin='https://localhost:'+port;
const setupToken=randomBytes(32).toString('hex'),password=randomBytes(20).toString('hex');
mkdirSync(join(root,'logs'));
mkdirSync(join(root,'temp'));
const snippet=readFileSync('deploy/native/nginx-existing.conf','utf8').replaceAll('127.0.0.1:8080','127.0.0.1:'+upstream);
const config=join(root,'nginx.conf');
const wrap=body=>`pid "${posix(join(root,'nginx.pid'))}"; error_log "${posix(join(root,'logs/error.log'))}"; events {}\nhttp { access_log off; ${['client_body','proxy','fastcgi','uwsgi','scgi'].map(name=>name+'_temp_path "'+posix(join(root,'temp',name))+'";').join(' ')} ${body} }\n`;
writeFileSync(config,wrap(`server { listen 127.0.0.1:${port} ssl; server_name localhost; ssl_certificate "${posix(cert)}"; ssl_certificate_key "${posix(key)}"; ${snippet} }`));
const children=[];
function launch(exe,args,env=process.env){const child=spawn(exe,args,{env,stdio:'ignore',windowsHide:true});child.on('error',()=>{});children.push(child);return child;}
let cookie='';
async function api(path,method='GET',body,status=200,headers={}){
  const r=await fetch(origin+'/api/v1'+path,{method,headers:{Origin:origin,Cookie:cookie,'Content-Type':'application/json',...headers},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(5000)});
  assert.equal(r.status,status,path);
  if(r.headers.get('set-cookie')){assert.match(r.headers.get('set-cookie'),/;\s*Secure(?:;|$)/i);cookie=r.headers.get('set-cookie').split(';')[0];}
  return (await r.json()).data;
}
async function ready(){for(let i=0;i<100;i++){try{if((await fetch(origin+'/api/v1/health',{signal:AbortSignal.timeout(1000)})).ok)return;}catch{}if(children.some(c=>c.exitCode!==null))throw Error('Proxy or API exited');await new Promise(r=>setTimeout(r,100));}throw Error('Proxy startup timed out');}
async function upgrade(path,headers){
  return await new Promise((resolve,reject)=>{
    const socket=tls.connect({host:'127.0.0.1',port,servername:'localhost',ca:readFileSync(cert)},()=>{
      socket.write(`GET ${path} HTTP/1.1\r\nHost: localhost:${port}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: ${randomBytes(16).toString('base64')}\r\n${Object.entries(headers).map(([k,v])=>k+': '+v+'\r\n').join('')}\r\n`);
    });
    let response='';socket.setTimeout(5000,()=>socket.destroy(Error('WebSocket upgrade timed out')));
    socket.on('data',chunk=>{response+=chunk.toString();if(response.includes('\r\n\r\n')){resolve(Number(response.split(' ')[1]));socket.destroy();}});
    socket.on('error',reject);
  });
}
try{
  for(const file of ['deploy/native/nginx.conf','deploy/nginx.conf','deploy/nginx.local.conf']){
    const template=readFileSync(file,'utf8').replaceAll('http://api:8080','http://127.0.0.1:'+upstream)
      .replaceAll('http://127.0.0.1:8080','http://127.0.0.1:'+upstream)
      .replace(/^(\s*ssl_certificate(_key)?\s+)[^;]+;/gm,(_,directive,isKey)=>directive+'"'+posix(isKey?key:cert)+'";')
      .replaceAll('/usr/share/nginx/html','"'+posix(webDir)+'"')
      .replaceAll('listen 80;','listen 127.0.0.1:18080;').replaceAll('listen 443 ssl;','listen 127.0.0.1:18443 ssl;');
    const check=join(root,'syntax.conf');writeFileSync(check,wrap(template));
    execFileSync(nginxPath,['-p',posix(root)+'/', '-c',posix(check),'-t'],{stdio:'pipe',windowsHide:true});
  }
  execFileSync(nginxPath,['-p',posix(root)+'/', '-c',posix(config),'-t'],{stdio:'pipe',windowsHide:true});
  launch(apiPath,[],{...process.env,DB_DRIVER:'sqlite',DATABASE_URL:'file:'+posix(join(root,'sshguard.db')),PUBLIC_URL:process.env.SSHGUARD_TEST_FIXED_ORIGIN === 'true' ? origin : '',
    ALLOW_INSECURE_HTTP:'false',TRUSTED_PROXY_CIDRS:'127.0.0.1/32,::1/128',TLS_CERT_FILE:'',TLS_KEY_FILE:'',WEB_SETUP:'true',SETUP_TOKEN:setupToken,SETUP_CONFIG_FILE:join(root,'setup.json'),
    ENCRYPTION_KEY:randomBytes(32).toString('hex'),LISTEN_ADDR:'127.0.0.1:'+upstream,WEB_DIR:webDir,GIN_MODE:'release'});
  launch(nginxPath,['-p',posix(root)+'/', '-c',posix(config),'-g','daemon off; master_process off;']);
  await ready();
  await api('/setup/complete','POST',{username:'admin',password},403);
  await api('/setup/complete','POST',{username:'admin',password},200,{'X-Setup-Token':setupToken});
  for(let i=0;i<100;i++){await new Promise(r=>setTimeout(r,100));try{if((await api('/health')).status==='ok')break;}catch{}}
  assert.equal((await api('/health')).status,'ok');
  await api('/auth/login','POST',{username:'admin',password},403,{Origin:'https://attacker.example'});
  await api('/auth/login','POST',{username:'admin',password});
  assert.equal(await upgrade('/api/v1/live',{Origin:origin,Cookie:cookie}),101);
  assert.equal(await upgrade('/api/v1/live',{Origin:'https://attacker.example',Cookie:cookie}),403);
  const {id}=await api('/servers','POST',{name:'HTTPS proxy fixture'});
  const registration=await api('/servers/'+id+'/enrollment','POST',{});
  assert.equal(registration.endpoint,origin);
  assert.equal(registration.insecure_http,false);
  assert(!registration.commands.ubuntu.includes('--allow-insecure-http'));
  const token=registration.commands.ubuntu.match(/'([a-f0-9]{64})'/)[1];
  const credential=await api('/agent/enroll','POST',{token,hostname:'proxy-fixture',distribution:'fixture',version:'0.0.0'});
  assert.equal(await upgrade('/api/v1/agent/stream',{Authorization:'Bearer '+credential.token}),101);
  assert.equal(await upgrade('/api/v1/agent/stream',{Authorization:'Bearer '+credential.token,Origin:origin}),403);
  assert.equal((await api('/agent/update-commands')).endpoint,origin);
  for(const path of ['/api.env','/setup.json','/downloads/.secret'])assert.equal((await fetch(origin+path)).status,404,path);
  const page=await fetch(origin+'/');assert.equal(page.status,200,'Console HTML unavailable');
  assert(page.headers.get('content-security-policy')?.includes("frame-ancestors 'none'"));
  const originPolicy=page.headers.get('content-security-policy');
  assert.equal(originPolicy.match(/default-src/g)?.length,1,'Proxy adds conflicting CSP headers');
  assert(originPolicy.includes("'nonce-"),'Origin HTML has no CSP nonce');
  const html=await page.text();
  assert.match(html,/<script\b[^>]*data-cfasync="false"[^>]*\bsrc=/,'Entry module is exposed to Rocket Loader rewriting');
  const staticPort=await freePort();
  const staticTemplate=readFileSync('deploy/nginx.local.conf','utf8')
    .replaceAll('http://api:8080','http://127.0.0.1:'+upstream)
    .replaceAll('/usr/share/nginx/html','"'+posix(webDir)+'"')
    .replaceAll('listen 80;','listen 127.0.0.1:'+staticPort+';');
  const staticConfig=join(root,'static.conf');
  writeFileSync(staticConfig,wrap(staticTemplate).replace('nginx.pid','static-nginx.pid'));
  launch(nginxPath,['-p',posix(root)+'/', '-c',posix(staticConfig),'-g','daemon off; master_process off;']);
  let staticPage;
  for(let i=0;i<100;i++){try{staticPage=await fetch('http://127.0.0.1:'+staticPort+'/dashboard');if(staticPage.ok)break;}catch{}await new Promise(r=>setTimeout(r,100));}
  assert(staticPage?.ok,'Static proxy did not start');
  const staticPolicy=staticPage.headers.get('content-security-policy');
  const nonce=staticPolicy?.match(/'nonce-([a-f0-9]+)'/)?.[1];
  assert(nonce&&nonce.length>=32,'Static proxy has no per-request nonce');
  assert.equal(staticPage.headers.get('cache-control'),'no-store','Nonce-bearing HTML can be cached');
  assert(staticPolicy.includes('https://static.cloudflareinsights.com/beacon.min.js/'));
  assert(!staticPolicy.match(/script-src [^;]+/)[0].includes("'unsafe-inline'"));
  const nextStaticPage=await fetch('http://127.0.0.1:'+staticPort+'/dashboard');
  assert(!nextStaticPage.headers.get('content-security-policy').includes("'nonce-"+nonce+"'"),'Static proxy reuses CSP nonces');
  console.log('Real Nginx HTTPS proxy passed: setup, Secure cookie, Origin rejection, browser and Agent WebSockets, exact PUBLIC_URL and file confinement.');
  console.log('Cloudflare CSP checks passed: fresh origin/static nonces, no conflicting policy, no cached HTML and Rocket Loader exclusion.');
}finally{
  for(const child of children.reverse())if(child.pid && child.exitCode===null){const done=new Promise(r=>child.once('exit',r));child.kill();await done;}
  rmSync(root,{recursive:true,force:true});
}
