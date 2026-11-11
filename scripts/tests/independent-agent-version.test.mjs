import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync, spawnSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {join,resolve,dirname} from 'node:path';
import {tmpdir} from 'node:os';
import {pathToFileURL} from 'node:url';
import {agentTag} from '../release/agent-version.mjs';

for (const [previous,changed,expected] of [['0.3.20',true,'0.4.0'],['0.3.22',false,'0.3.22'],['0.3.22',true,'0.4.0'],['0.4.0',true,'0.4.1'],['0.4.1',false,'0.4.1']]) {
 test(`controller 0.3.23, Agent ${previous}, changed=${changed} => ${expected}`,()=>{
  const dir=mkdtempSync(join(tmpdir(),'sshguard-independent-'));
  const git=(...args)=>execFileSync('git',args,{cwd:dir,encoding:'utf8',stdio:['pipe','pipe','pipe']}).trim();
  const put=(file,value)=>{mkdirSync(dirname(join(dir,file)),{recursive:true});writeFileSync(join(dir,file),value);};
  try {
   const files={VERSION:'0.3.22\n',AGENT_VERSION:previous+'\n',
    'internal/api/routes.go':`var Version = "0.3.22"\nvar AgentVersion = "${previous}"\n`,
    'internal/agent/run.go':`var version = "${previous}"\n`,
    'scripts/install-agent.sh':`RELEASE_VERSION='${previous}'\n`,
    'scripts/install-agent.ps1':`$ReleaseVersion = '${previous}'\n`,
    'sshguard.sh':'version=${SSHGUARD_VERSION:-v0.3.22}\n',
    'CHANGELOG.md':'# Changes\n', 'README.md':'# Readme\n',
    'docs/guides/QUICKSTART.md':'', 'docs/deployment/INSTALL-NATIVE.md':'', 'docs/development/RELEASING.md':''};
   for(const [name,value] of Object.entries(files))put(name,value);
   git('init','-b','main');git('config','user.name','Test');git('config','user.email','test@example.invalid');
   git('add','.');git('commit','-m','initial');git('tag',agentTag(previous));git('tag','v0.3.22');
   // A controller tag with the next Agent version already exists. It must not be used for Agent downloads or block its version.
   if(previous==='0.3.20')git('tag','v0.4.0');
   put(changed?'internal/agent/feature.go':'internal/api/feature.go','package feature\n');
   git('add','.');git('commit','-m','feature');
   put('.git/mock.mjs',`import cp from 'node:child_process';import {syncBuiltinESMExports} from 'node:module';const real=cp.execFileSync;cp.execFileSync=(cmd,args,opts)=>cmd==='gh'?JSON.stringify([[{tag_name:'v0.3.22'}]]):real(cmd,args,opts);syncBuiltinESMExports();`);
   const result=spawnSync(process.execPath,['--import',pathToFileURL(join(dir,'.git/mock.mjs')).href,resolve('scripts/release/prepare-release.mjs')],{cwd:dir,encoding:'utf8',env:{...process.env,GITHUB_EVENT_NAME:'workflow_dispatch',GITHUB_REF_TYPE:'branch',GITHUB_REPOSITORY:'example/repo',REQUESTED_VERSION:'',GITHUB_OUTPUT:join(dir,'.git/output'),GITHUB_STEP_SUMMARY:join(dir,'.git/summary')}});
   assert.equal(result.status,0,result.stderr);
   assert.equal(readFileSync(join(dir,'VERSION'),'utf8').trim(),'0.3.23');
   assert.equal(readFileSync(join(dir,'AGENT_VERSION'),'utf8').trim(),expected);
   assert.ok(readFileSync(join(dir,'scripts/install-agent.sh'),'utf8').includes(`RELEASE_VERSION='${expected}'`));
  } finally {rmSync(dir,{recursive:true,force:true});}
 });
}
