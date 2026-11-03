import {execFileSync} from 'node:child_process';
export function agentTag(version) {
 const parts = version.split('.').map(Number);
 return (parts[0] === 0 && parts[1] <= 3 && (parts[1] < 3 || parts[2] <= 20) ? 'v' : 'agent-v') + version;
}
export function agentTagExists(version) {
 try { execFileSync('git',['rev-parse','--verify',`refs/tags/${agentTag(version)}^{commit}`],{stdio:'pipe'}); return true; }
 catch { return false; }
}
export function affectsAgent(path) {
 if (path.endsWith('_test.go') || path.startsWith('scripts/tests/')) return false;
 return ['go.mod','go.sum','scripts/build/build-agents.mjs','scripts/release/agent-version.mjs'].includes(path) ||
 /^(cmd\/agent|internal\/(agent|ipaddress|originurl))\//.test(path) || /^scripts\/(install|uninstall)-agent\.(sh|ps1)$/.test(path);
}
export function agentChanged(version) {
 try {
  const tag = agentTag(version);
  execFileSync('git',['merge-base','--is-ancestor',tag,'HEAD'],{stdio:'pipe'});
  return execFileSync('git',['diff','--name-only',tag],{encoding:'utf8'}).trim().split('\n').some(affectsAgent);
 } catch { return true; } // Missing history must never silently reuse old code.
}
