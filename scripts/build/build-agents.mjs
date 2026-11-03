import { agentChanged, agentTag, agentTagExists } from '../release/agent-version.mjs';
import { execFileSync } from 'node:child_process';
import { mkdirSync, readFileSync, writeFileSync, cpSync } from 'node:fs';
import { resolve, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
const root = fileURLToPath(new URL('../../', import.meta.url));
process.chdir(root);
const out = resolve(process.argv[2] || 'dist/agents');
const rel = relative(resolve('dist'), out);
if (!rel || rel.startsWith('..') || resolve('dist', rel) !== out) throw Error('Agent output must be a subdirectory of dist');
const version = process.env.SSHGUARD_AGENT_VERSION || readFileSync('AGENT_VERSION','utf8').trim();
if (!/^[a-zA-Z0-9.+_-]{1,32}$/.test(version)) throw Error('Invalid version');
mkdirSync(out,{recursive:true});
if (agentTagExists(version) && !process.env.SSHGUARD_AGENT_VERSION) {
  if (agentChanged(version)) throw Error('Agent sources changed: bump AGENT_VERSION before releasing');
  execFileSync('gh',['release','download',agentTag(version),'--repo','xiuzhenzhi/SSHGuard','--pattern','sshguard-agent-*','--dir',out,'--clobber'],{stdio:'inherit'});
  execFileSync(process.execPath,['scripts/checks/check-agent-artifacts.mjs',out],{stdio:'inherit'});
  console.log('Reused published Agent '+version+'; skipped cross compilation');
} else for (const os of ['linux','windows']) for (const arch of ['amd64','arm64']) {
  const name = `sshguard-agent-${os}-${arch}${os === 'windows' ? '.exe' : ''}`;
  // Agent HTTPS and WebSocket traffic uses HTTP/1.1; omit the unused HTTP/2 stack.
  execFileSync('go',['build','-trimpath','-tags=nethttpomithttp2','-ldflags',`-s -w -X sshguard/internal/agent.version=${version}`,'-o',resolve(out,name),'./cmd/agent'],{stdio:'inherit',env:{...process.env,CGO_ENABLED:'0',GOOS:os,GOARCH:arch}});
  writeFileSync(resolve(out,`${name}.sha256`),`${createHash('sha256').update(readFileSync(resolve(out,name))).digest('hex')}  ${name}\n`);
}
for (const name of ['install-agent.sh','install-agent.ps1','uninstall-agent.sh','uninstall-agent.ps1']) cpSync(`scripts/${name}`,resolve(out,name));
console.log(`Agent ${version}: linux/windows × amd64/arm64 → ${out}`);
