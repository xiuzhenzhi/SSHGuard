import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {join} from 'node:path';

const shell = process.env.SSHGUARD_TEST_SHELL || (process.platform === 'win32' ? join(process.env.ProgramFiles, 'Git/bin/sh.exe') : 'sh');
const source = readFileSync('sshguard.sh', 'utf8');
const fn = source.slice(source.indexOf('direct_origin() {'), source.indexOf('\nif [ "${1:-}" = --help ]'));
for (const [name, ssh, ips, want] of [
  ['NIC address rather than SSH endpoint', '10.0.0.1 22 10.0.0.2 22', '8.8.4.4 10.0.0.2', 'http://8.8.4.4:8080'],
  ['first address matches Komari', '', '192.168.1.2 8.8.8.8', 'http://192.168.1.2:8080'],
  ['IPv6', '', '2606:4700::1111', 'http://[2606:4700::1111]:8080'],
  ['no address', '', '', 'http://127.0.0.1:8080'],
]) {
  test(name, () => {
    const result = spawnSync(shell, ['-c', `set -eu
${fn}
hostname() { printf '%s' "$TEST_IPS"; }
curl() { exit 99; }
direct_origin 8080`], {encoding:'utf8', env:{...process.env, SSH_CONNECTION:ssh, TEST_IPS:ips}});
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout, want);
  });
}

test('updates permit explicit IPv6 origins outside managed Nginx mode', () => {
  const start = source.indexOf('if [ "$proxy_mode" = managed ]; then\n    printf');
  const end = source.indexOf('\nprintf', start);
  assert(start > 0 && end > start);
  const validation = source.slice(start, end);
  for (const mode of ['standalone', 'external', 'managed']) {
    const result = spawnSync(shell, ['-c', `set -eu
proxy_mode=$TEST_MODE; domain='[2001:db8::1]'; public_origin='https://[2001:db8::1]:8443'; app_port=8443
die() { exit 42; }
${validation}`], {encoding:'utf8', env:{...process.env,TEST_MODE:mode}});
    assert.equal(result.status, mode === 'managed' ? 42 : 0, result.stderr);
  }
});
