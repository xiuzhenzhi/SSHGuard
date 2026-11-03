// Publish only the Agent assets that passed the controller release checks.
import { execFileSync, spawnSync } from 'node:child_process';
import { readFileSync, mkdtempSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { agentTag } from './agent-version.mjs';
import { parseVersion } from './release-version.mjs';

const version = parseVersion(readFileSync('AGENT_VERSION','utf8')).version;
const tag = agentTag(version);
const repo = process.env.GITHUB_REPOSITORY;
if (!/^[\w.-]+\/[\w.-]+$/.test(repo || '')) throw Error('Invalid repository');
const files = ['AGENT_VERSION','install-agent.sh','install-agent.ps1','uninstall-agent.sh','uninstall-agent.ps1'];
for (const os of ['linux','windows']) for (const arch of ['amd64','arm64']) {
 const name = `sshguard-agent-${os}-${arch}${os === 'windows' ? '.exe' : ''}`;
 files.push(name, name+'.sha256');
}
const gh = (...args) => execFileSync('gh',args,{stdio:'inherit'});
const existing = spawnSync('gh',['release','view',tag,'--repo',repo],{stdio:'pipe'});
if (existing.status === 0) {
 const dir = mkdtempSync(join(tmpdir(),'sshguard-agent-release-'));
 try {
  gh('release','download',tag,'--repo',repo,'--dir',dir);
  for (const name of files) if (!readFileSync(join(dir,name)).equals(readFileSync(join('dist/release',name)))) {
   throw Error('Published Agent assets differ: '+name+'; never overwrite them.');
  }
 } finally { rmSync(dir,{recursive:true,force:true}); }
 console.log('Verified existing Agent '+version);
} else {
 // A network/auth failure is not proof of absence. Confirm the API reports 404.
 const check = spawnSync('gh',['api',`repos/${repo}/releases/tags/${tag}`],{encoding:'utf8'});
 if (!check.stderr?.includes('(HTTP 404)')) throw Error('Cannot verify Agent release absence: '+check.stderr);
 const sha = execFileSync('git',['rev-parse','HEAD'],{encoding:'utf8'}).trim();
 gh('release','create',tag,...files.map(n=>join('dist/release',n)), '--repo',repo,'--target',sha,'--latest=false',
  ...(version.includes('-')?['--prerelease']:[]),'--title',`SSHGuard Agent v${version}`,
  '--notes',`Agent v${version}. Independent Agent version; controller releases use v* tags. See the online Agent guide for installation and upgrade instructions.`);
}
