import test from 'node:test';
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {join,resolve} from 'node:path';
import {tmpdir} from 'node:os';
import {pathToFileURL} from 'node:url';

for(const mode of ['new','existing','mismatch','network'])test('Agent publishing: '+mode,()=>{
 const dir=mkdtempSync(join(tmpdir(),'sshguard-publish-agent-'));
 try {
  mkdirSync(join(dir,'dist/release'),{recursive:true});writeFileSync(join(dir,'AGENT_VERSION'),'0.3.21\n');
  const files=['AGENT_VERSION','install-agent.sh','install-agent.ps1','uninstall-agent.sh','uninstall-agent.ps1'];
  for(const os of ['linux','windows'])for(const arch of ['amd64','arm64']){const n=`sshguard-agent-${os}-${arch}${os==='windows'?'.exe':''}`;files.push(n,n+'.sha256');}
  for(const n of files)writeFileSync(join(dir,'dist/release',n),n);
  writeFileSync(join(dir,'mock.mjs'),`import cp from 'node:child_process';import {syncBuiltinESMExports} from 'node:module';import {writeFileSync,cpSync,readdirSync} from 'node:fs';import {join} from 'node:path';
cp.spawnSync=(_,args)=>args[0]==='api'?{status:1,stderr:${JSON.stringify(mode==='network'?'connection failed':'gh: Not Found (HTTP 404)')}}:{status:${mode==='existing'||mode==='mismatch'?0:1}};
cp.execFileSync=(cmd,args)=>{if(cmd==='git')return 'abc123';if(args[1]==='download'){const dir=args[args.indexOf('--dir')+1];for(const n of readdirSync('dist/release'))cpSync(join('dist/release',n),join(dir,n));${mode==='mismatch'?'writeFileSync(join(dir,"install-agent.sh"),"changed");':''}return '';}writeFileSync('created.json',JSON.stringify(args));};syncBuiltinESMExports();`);
  const result=spawnSync(process.execPath,['--import',pathToFileURL(join(dir,'mock.mjs')).href,resolve('scripts/release/publish-agent.mjs')],{cwd:dir,encoding:'utf8',env:{...process.env,GITHUB_REPOSITORY:'example/repo'}});
  assert.equal(result.status,mode==='mismatch'||mode==='network'?1:0,result.stderr);
  if(mode==='new'){
   const args=JSON.parse(readFileSync(join(dir,'created.json'),'utf8'));
   assert.equal(args[2],'agent-v0.3.21');assert.ok(args.includes('--latest=false'));
   assert.ok(args.includes('--verify-tag'));assert.ok(!args.includes('--target'));
   assert.ok(!args.some(v=>v.includes('sshguard-linux-')));
  }
  if(mode==='mismatch')assert.match(result.stderr,/never overwrite/);
  if(mode==='network')assert.match(result.stderr,/Cannot verify/);
 } finally {rmSync(dir,{recursive:true,force:true});}
});
