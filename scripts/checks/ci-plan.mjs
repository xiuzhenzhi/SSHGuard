import { execFileSync } from 'node:child_process';
import { appendFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

const versionPatterns = {
 VERSION: /^[0-9]+\.[0-9]+\.[0-9]+(?:-[\w.-]+)?\r?\n?$/,
 'internal/api/routes.go': /^var (?:Agent)?Version = "[0-9]+\.[0-9]+\.[0-9]+(?:-[\w.-]+)?"$/gm,
 'sshguard.sh': /^version=\$\{SSHGUARD_VERSION:-v[0-9]+\.[0-9]+\.[0-9]+(?:-[\w.-]+)?\}$/m,
};

// Ignore only a changed release value, never another edit in the same file.
export function isVersionOnlyChange(path, before, after) {
 const pattern=versionPatterns[path];
 if (!pattern || before===after) return false;
 const oldMatches=before.match(pattern),newMatches=after.match(pattern);
 if(!oldMatches || !newMatches || oldMatches.length!==newMatches.length)return false;
 return before.replace(pattern,'<release-version>')===after.replace(pattern,'<release-version>');
}

export function isDatabaseConsumer(source) {
 return /\b(?:db|tx)\.(?:Row|List|Exec|Query|Begin|Conn)\w*\s*\(|\bstorage\.(?:Open|Migrate|Cleanup)\s*\(/.test(source);
}

export function planCI(files, full = false, versionOnly = [], databaseLinked = []) {
 const plan = {server:false, agent:false, database:false, web:false, deploy:false, automation:false, docs:false};
 if (full || files === null) return Object.fromEntries(Object.keys(plan).map(k=>[k,true]));
 for (const p of files) {
  if (versionOnly.includes(p) && Object.hasOwn(versionPatterns,p)) continue;
  if (databaseLinked.includes(p)) plan.database=true;
  if (/\.md$/i.test(p) || p.startsWith('docs/') || ['DOCS_VERSION','LICENSE','package.json','package-lock.json','.github/workflows/docs.yml','scripts/build/build-docs.mjs','scripts/checks/check-docs.mjs'].includes(p)) {plan.docs=true;continue;}
  if (['scripts/checks/ci-plan.mjs','scripts/tests/ci-plan.test.mjs'].includes(p)) continue; // Always tested by the scope job.
  if (p === '.github/workflows/sqlite-install.yml') {plan.automation=true;continue;}
  if (p.startsWith('web/')) {plan.web=true;continue;}
  if (['go.mod','go.sum'].includes(p)) {plan.server=true;plan.agent=true;plan.database=true;continue;}
  if (/^(cmd\/agent|internal\/agent)\//.test(p)) {plan.agent=true;continue;}
  if (/^internal\/(ipaddress|originurl)\//.test(p)) {plan.server=true;plan.agent=true;continue;}
  if (p.startsWith('internal/storage/') || /^internal\/api\/(database|retention)(_|\.)/.test(p)) {plan.database=true;continue;}
  if (/^(internal\/(api|server|geoip|notify)|cmd\/api)\//.test(p) || p==='VERSION') {plan.server=true;continue;}
  if (/^scripts\/(install|uninstall)-agent\./.test(p) || p==='AGENT_VERSION' || ['scripts/build/build-agents.mjs','scripts/checks/check-agent-artifacts.mjs','scripts/release/agent-version.mjs'].includes(p) || /^scripts\/tests\/test-(windows|alpine|agent)/.test(p)) {plan.agent=true;continue;}
  if (p==='deploy/Dockerfile.api') {plan.server=true;continue;}
  if (p==='deploy/Dockerfile.web') {plan.web=true;continue;}
  if (p.startsWith('deploy/') || ['compose.yaml','.env.example','sshguard.sh','scripts/deploy.sh','scripts/update.sh','scripts/quick-start.sh','scripts/uninstall.sh','scripts/dev/init-env.mjs'].includes(p) || /^scripts\/tests\/(quick-start|test-native|test-direct-install|test-reverse-proxy)/.test(p)) {plan.deploy=true;plan.automation=true;continue;}
  if (p.startsWith('scripts/release/') || p.startsWith('scripts/build/') || p.startsWith('scripts/tests/')) {plan.automation=true;continue;}
  // Workflow and unknown changes retain full verification.
  return Object.fromEntries(Object.keys(plan).map(k=>[k,true]));
 }
 return plan;
}

export function nativeMatrix(plan, full=false) {
 const sqlite=['managed','external','standalone','standalone-http'].map(proxy=>({database:'sqlite',proxy}));
 if(full||plan.database||plan.deploy)return {include:[{database:'local-postgres',proxy:'managed'},...sqlite]};
 if(plan.server)return {include:sqlite};
 if(plan.agent)return {include:[{database:'sqlite',proxy:'standalone-http'}]};
 return {include:[]};
}

export function validatedBase(runs, head, isAncestor) {
 for(const run of runs){
  if(run.conclusion!=='success' || run.head_sha===head || !/^[a-f0-9]{40}$/.test(run.head_sha||''))continue;
  if(isAncestor(run.head_sha))return run.head_sha;
 }
 return '';
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  let base = process.env.PR_BASE_SHA || process.env.BEFORE_SHA || '';
  const head = process.env.SOURCE_SHA || process.env.GITHUB_SHA || '';
  if (process.env.SELECTIVE_RELEASE === 'true') {
    try { base = execFileSync('git',['rev-list','-n','1',execFileSync('git',['describe','--tags','--match','v[0-9]*','--abbrev=0',head+'^'],{encoding:'utf8'}).trim()],{encoding:'utf8'}).trim(); } catch { base=''; }
  } else if(process.env.GITHUB_EVENT_NAME==='push' && process.env.GITHUB_REF_TYPE==='branch') {
    // A newer push can cancel its predecessor. Compare against a successful CI,
    // not the immediately preceding (possibly unverified) commit.
    base='';
    try {
      const repo=process.env.GITHUB_REPOSITORY,branch=process.env.GITHUB_REF_NAME;
      if(!/^[\w.-]+\/[\w.-]+$/.test(repo||'') || !branch)throw Error('Missing repository context');
      const response=await fetch(`https://api.github.com/repos/${repo}/actions/workflows/ci.yml/runs?branch=${encodeURIComponent(branch)}&event=push&status=success&per_page=30`,{
        headers:{Authorization:`Bearer ${process.env.GH_TOKEN}`,Accept:'application/vnd.github+json'},signal:AbortSignal.timeout(15000),
      });
      if(!response.ok)throw Error('Cannot read validated history');
      const data=await response.json();
      base=validatedBase(data.workflow_runs||[],head,sha=>{
        try{execFileSync('git',['merge-base','--is-ancestor',sha,head],{stdio:'ignore'});return true;}catch{return false;}
      });
    }catch{ /* Unknown history expands checks instead of losing cancelled changes. */ }
  }
  let files = null;
  if (/^[a-f0-9]{40}$/.test(base) && !/^0+$/.test(base) && /^[a-f0-9]{40}$/.test(head)) {
    try { files = execFileSync('git', ['diff', '--name-only', '-z', base, head], {encoding:'utf8'}).split('\0').filter(Boolean); }
    catch { /* A missing base must expand checks, never silently skip them. */ }
  }
  const versionOnly=[];
  for (const path of files || []) {
    if (!Object.hasOwn(versionPatterns,path)) continue;
    try {
      const before=execFileSync('git',['show',`${base}:${path}`],{encoding:'utf8',stdio:['ignore','pipe','ignore']});
      const after=execFileSync('git',['show',`${head}:${path}`],{encoding:'utf8',stdio:['ignore','pipe','ignore']});
      if (isVersionOnlyChange(path,before,after)) versionOnly.push(path);
    } catch { /* Missing or unreadable content keeps its normal checks. */ }
  }
  const databaseLinked=[];
  for (const path of files || []) {
    if (!/^internal\/(api|server)\/.*\.go$/.test(path) || versionOnly.includes(path)) continue;
    try {
      const sources=[base,head].map(ref=>{
        try{return execFileSync('git',['show',`${ref}:${path}`],{encoding:'utf8',stdio:['ignore','pipe','ignore']});}
        catch{return null;}
      });
      if(sources.every(source=>source===null)||sources.some(source=>source!==null&&isDatabaseConsumer(source)))databaseLinked.push(path);
    } catch {databaseLinked.push(path);}
  }
  const plan = planCI(files, process.env.FULL_VALIDATION === 'true' && process.env.SELECTIVE_RELEASE !== 'true',versionOnly,databaseLinked);
  appendFileSync(process.env.GITHUB_OUTPUT, Object.entries(plan).map(([k,v])=>`${k}=${v}\n`).join(''));
  appendFileSync(process.env.GITHUB_OUTPUT, `native_matrix=${JSON.stringify(nativeMatrix(plan))}\n`);
  console.log(JSON.stringify({files: files?.length ?? 'all', versionOnly, databaseLinked, ...plan}));
}
