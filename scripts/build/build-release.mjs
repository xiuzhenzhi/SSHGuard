import { execFileSync } from 'node:child_process';
import { mkdirSync, cpSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { resolve, relative } from 'node:path';
const arch = process.argv[2] || 'amd64';
if (!['amd64','arm64'].includes(arch)) throw Error('Choose amd64 or arm64');
const root = new URL('../../', import.meta.url);
process.chdir(fileURLToPath(root));
const out = `dist/sshguard-linux-${arch}`;
// Always rebuild a clean bundle so obsolete files (including old docs) cannot ship.
const output = resolve(out);
if (relative(resolve('dist'), output).replaceAll('\\','/') !== `sshguard-linux-${arch}`) throw Error('Invalid release output path');
rmSync(output, {recursive:true, force:true});
mkdirSync(`${out}/web/downloads`, {recursive:true});
const agentVersion = process.env.SSHGUARD_AGENT_VERSION || readFileSync('AGENT_VERSION','utf8').trim();
const version = process.env.SSHGUARD_VERSION || readFileSync('VERSION','utf8').trim();
if (!/^[a-zA-Z0-9.+_-]{1,32}$/.test(version)) throw Error('Invalid version');
execFileSync('go', ['build','-trimpath','-ldflags',`-s -w -X sshguard/internal/api.Version=${version} -X sshguard/internal/api.AgentVersion=${agentVersion}`,'-o',`${out}/sshguard-api`,'./cmd/api'], {stdio:'inherit',env:{...process.env,CGO_ENABLED:'0',GOOS:'linux',GOARCH:arch}});
if (process.argv.includes('--reuse-agents')) {
  const source='dist/sshguard-linux-amd64';
  if (arch!=='arm64' || readFileSync(`${source}/VERSION`,'utf8').trim()!==version || readFileSync(`${source}/AGENT_VERSION`,'utf8').trim()!==agentVersion) throw Error('Agent reuse requires the matching amd64 package built first');
  execFileSync(process.execPath,['scripts/checks/check-agent-artifacts.mjs',`${source}/web/downloads`],{stdio:'inherit'});
  for(const os of ['linux','windows']) for(const cpu of ['amd64','arm64']) {
    const name=`sshguard-agent-${os}-${cpu}${os==='windows'?'.exe':''}`;
    for(const suffix of ['', '.sha256']) cpSync(`${source}/web/downloads/${name}${suffix}`,`${out}/web/downloads/${name}${suffix}`);
  }
} else {
  execFileSync(process.execPath,['scripts/build/build-agents.mjs',`${out}/web/downloads`],{stdio:'inherit'});
}
// npm.cmd needs cmd.exe on Windows; arguments here are fixed literals.
const npm = (args) => process.platform === 'win32' ? execFileSync('cmd.exe',['/d','/s','/c',`npm.cmd ${args.join(' ')}`],{cwd:'web',stdio:'inherit'}) : execFileSync('npm',args,{cwd:'web',stdio:'inherit'});
if (!process.argv.includes('--reuse-web')) { npm(['ci','--no-audit','--no-fund']); npm(['run','build']); }
cpSync('web/dist',`${out}/web`,{recursive:true});
for (const name of ['install-agent.sh','install-agent.ps1','uninstall-agent.sh','uninstall-agent.ps1']) {
  let source=readFileSync(`scripts/${name}`,'utf8');
  source=source.replace(/RELEASE_VERSION='[^']+'/,()=>`RELEASE_VERSION='${agentVersion}'`).replace(/\$ReleaseVersion = '[^']+'/,()=>`$ReleaseVersion = '${agentVersion}'`);
  source=source.replace(/\r\n/g,'\n');
  writeFileSync(`${out}/web/downloads/${name}`,name.endsWith('.ps1')?source.replace(/\n/g,'\r\n'):source);
}
cpSync('deploy/native',`${out}/deploy`,{recursive:true});
// The manifest must match the binary and Agents, including preview overrides.
writeFileSync(`${out}/VERSION`,version+'\n');
writeFileSync(`${out}/AGENT_VERSION`,agentVersion+'\n');
cpSync('sshguard.sh',`${out}/sshguard.sh`);
cpSync('scripts/deploy.sh',`${out}/deploy.sh`);
cpSync('scripts/quick-start.sh',`${out}/quick-start.sh`);
cpSync('scripts/update.sh',`${out}/update.sh`);
cpSync('scripts/uninstall.sh',`${out}/uninstall.sh`);
console.log(`Native Linux release ready: ${out}`);
