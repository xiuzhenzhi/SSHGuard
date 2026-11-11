import test from 'node:test';
import assert from 'node:assert/strict';
import {planCI,isVersionOnlyChange,nativeMatrix,isDatabaseConsumer,validatedBase} from '../checks/ci-plan.mjs';
import {affectsAgent} from '../release/agent-version.mjs';
const none={server:false,agent:false,database:false,web:false,deploy:false,automation:false,docs:false};
const all=Object.fromEntries(Object.keys(none).map(k=>[k,true]));

test('cancelled or unrelated commits cannot hide unverified changes',()=>{
 const head='a'.repeat(40),cancelled='b'.repeat(40),unrelated='c'.repeat(40),passed='d'.repeat(40);
 const runs=[{head_sha:head,conclusion:'success'},{head_sha:cancelled,conclusion:'cancelled'},{head_sha:unrelated,conclusion:'success'},{head_sha:passed,conclusion:'success'}];
 assert.equal(validatedBase(runs,head,sha=>sha===passed),passed);
 assert.equal(validatedBase(runs,head,()=>false),'');
});

test('release version edits do not expand a frontend-only change',()=>{
 const fixtures=[
  ['VERSION','0.3.16\n','0.3.17\n'],
  ['internal/api/routes.go','package api\nvar Version = "0.3.16"\nfunc Route() {}\n','package api\nvar Version = "0.3.17"\nfunc Route() {}\n'],
  ['sshguard.sh','version=${SSHGUARD_VERSION:-v0.3.16}\nrun\n','version=${SSHGUARD_VERSION:-v0.3.17}\nrun\n'],
 ];
 for(const [path,before,after] of fixtures){
  assert.equal(isVersionOnlyChange(path,before,after),true,path);
  assert.equal(isVersionOnlyChange(path,before,after+'extra logic\n'),false,path);
  assert.equal(isVersionOnlyChange(path,'',after),false,path);
 }
 const paths=fixtures.map(([path])=>path);
 assert.deepEqual(planCI([...paths,'web/src/App.vue'],false,paths),{...none,web:true});
 assert.deepEqual(planCI(paths,true,paths),all);
 assert.deepEqual(planCI(['internal/api/routes.go']),{...none,server:true});
 assert.equal(isVersionOnlyChange('internal/api/routes.go','var AgentVersion = "0.3.16"','var AgentVersion = "0.3.17"'),true);
 assert.equal(isVersionOnlyChange('internal/api/routes.go','var Version = "0.3.16"','var Version = "0.3.17"\nvar Version = "0.3.17"'),false);
 assert.deepEqual(planCI(['AGENT_VERSION','internal/api/routes.go'],false,['internal/api/routes.go']),{...none,agent:true});
});
test('component checks stay isolated',()=>{
 for(const p of ['DOCS_VERSION','README.md','docs/guides/AGENTS.md','docs/.vitepress/config.mts','.github/workflows/docs.yml'])assert.deepEqual(planCI([p]),{...none,docs:true},p);
 for(const p of ['scripts/checks/ci-plan.mjs','scripts/tests/ci-plan.test.mjs'])assert.deepEqual(planCI([p]),none,p);
 assert.deepEqual(planCI(['web/src/App.vue']),{...none,web:true});
 assert.deepEqual(planCI(['internal/agent/run.go']),{...none,agent:true});
 assert.deepEqual(planCI(['internal/api/auth.go']),{...none,server:true});
 assert.deepEqual(planCI(['internal/storage/migrations/sqlite/012.sql']),{...none,database:true});
 assert.deepEqual(planCI(['internal/notify/send.go']),{...none,server:true});
 assert.deepEqual(planCI(['scripts/uninstall-agent.sh']),{...none,agent:true});
 assert.deepEqual(planCI(['deploy/nginx.conf']),{...none,deploy:true,automation:true});
});
test('shared dependencies, unknown paths and explicit full validation fail safe',()=>{
 for(const p of ['.github/workflows/ci.yml','unknown'])assert.deepEqual(planCI([p]),all);
 assert.deepEqual(planCI(null),all);assert.deepEqual(planCI([],true),all);
});
test('only production Agent inputs advance Agent version',()=>{
 for(const p of ['internal/agent/run.go','internal/ipaddress/public.go','cmd/agent/main.go','go.mod','scripts/uninstall-agent.ps1'])assert.equal(affectsAgent(p),true,p);
 for(const p of ['internal/api/auth.go','web/src/App.vue','VERSION','docs/index.md','internal/agent/run_test.go'])assert.equal(affectsAgent(p),false,p);
});

test('shared Go dependencies check only Go consumers; mixed edits form a union',()=>{
 assert.deepEqual(planCI(['go.mod']),{...none,server:true,agent:true,database:true});
 assert.deepEqual(planCI(['internal/api/database.go']),{...none,database:true});
 assert.deepEqual(planCI(['internal/api/retention_test.go']),{...none,database:true});
 assert.deepEqual(planCI(['web/src/App.vue','docs/index.md']),{...none,web:true,docs:true});
 assert.deepEqual(planCI(['internal/agent/transport.go','internal/storage/storage.go']),{...none,agent:true,database:true});
 assert.deepEqual(planCI(['scripts/build/package-release.mjs']),{...none,automation:true});
});

test('native release checks follow the affected runtime only',()=>{
 assert.deepEqual(nativeMatrix({...none,web:true}),{include:[]});
 assert.deepEqual(nativeMatrix({...none,agent:true}),{include:[{database:'sqlite',proxy:'standalone-http'}]});
 assert.equal(nativeMatrix({...none,server:true}).include.length,4);
 assert.equal(nativeMatrix({...none,database:true}).include.length,5);
 assert.equal(nativeMatrix(none,true).include.length,5);
});

test('database consumers retain integration coverage when handlers change',()=>{
 assert.equal(isDatabaseConsumer('a.db.Row(ctx, query)'),true);
 assert.equal(isDatabaseConsumer('tx.ExecContext(ctx, query)'),true);
 assert.equal(isDatabaseConsumer('storage.Migrate(ctx, db)'),true);
 assert.equal(isDatabaseConsumer('c.JSON(200, result)'),false);
 assert.deepEqual(planCI(['internal/api/auth.go'],false,[],['internal/api/auth.go']),{...none,server:true,database:true});
 assert.deepEqual(planCI(['internal/api/routes.go'],false,['internal/api/routes.go'],['internal/api/routes.go']),none);
});

test('all 128 module combinations preserve the exact union of affected checks',()=>{
 const components=[
  ['internal/api/web.go',{server:true}],['internal/agent/transport.go',{agent:true}],
  ['internal/storage/storage.go',{database:true}],['web/src/App.vue',{web:true}],
  ['deploy/nginx.conf',{deploy:true,automation:true}],['docs/index.md',{docs:true}],
  ['scripts/build/package-release.mjs',{automation:true}],
 ];
 for(let mask=0;mask<1<<components.length;mask++){
  const files=[],expected={...none};
  components.forEach(([path,flags],i)=>{if(mask&(1<<i)){files.push(path);Object.assign(expected,flags);}});
  assert.deepEqual(planCI(files),expected,JSON.stringify(files));
 }
});

test('real Git diffs keep frontend-only and Agent + database + docs releases isolated',async()=>{
 const {mkdtempSync,mkdirSync,writeFileSync,rmSync}=await import('node:fs');
 const {tmpdir}=await import('node:os');const {join,dirname}=await import('node:path');
 const {execFileSync}=await import('node:child_process');const {fileURLToPath}=await import('node:url');
 const root=mkdtempSync(join(tmpdir(),'sshguard-ci-scope-'));
 const git=(...args)=>execFileSync('git',args,{cwd:root,encoding:'utf8',stdio:['ignore','pipe','pipe']}).trim();
 const put=(path,text)=>{mkdirSync(dirname(join(root,path)),{recursive:true});writeFileSync(join(root,path),text);};
 const commit=()=>{git('add','.');git('-c','user.name=CI fixture','-c','user.email=fixture@example.invalid','commit','-m','fixture');return git('rev-parse','HEAD');};
 const run=base=>JSON.parse(execFileSync(process.execPath,[fileURLToPath(new URL('../checks/ci-plan.mjs',import.meta.url))],{cwd:root,encoding:'utf8',env:{...process.env,GITHUB_EVENT_NAME:'test',GITHUB_REF_TYPE:'',BEFORE_SHA:base,PR_BASE_SHA:'',SOURCE_SHA:git('rev-parse','HEAD'),SELECTIVE_RELEASE:'false',FULL_VALIDATION:'false',GITHUB_OUTPUT:join(root,'result.txt')}}));
 try{
  git('init');put('.gitignore','result.txt\n');put('VERSION','0.3.16\n');put('AGENT_VERSION','0.3.16\n');put('internal/api/routes.go','package api\nvar Version = "0.3.16"\nvar AgentVersion = "0.3.16"\n');put('web/src/App.vue','old');put('internal/storage/storage.go','package storage\n');put('docs/index.md','old');
  const before=commit();put('VERSION','0.3.17\n');put('internal/api/routes.go','package api\nvar Version = "0.3.17"\nvar AgentVersion = "0.3.16"\n');put('web/src/App.vue','new');const frontend=commit();
  const first=run(before);for(const key of Object.keys(none))assert.equal(first[key],key==='web',key);
  put('AGENT_VERSION','0.3.18\n');put('internal/api/routes.go','package api\nvar Version = "0.3.17"\nvar AgentVersion = "0.3.18"\n');put('internal/storage/storage.go','package storage\n// changed storage\n');put('docs/index.md','new');commit();
  const second=run(frontend);for(const key of Object.keys(none))assert.equal(second[key],['agent','database','docs'].includes(key),key);
 }finally{rmSync(root,{recursive:true,force:true});}
});
