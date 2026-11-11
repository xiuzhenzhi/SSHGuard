import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

for (const mode of ['legacy','unchanged','changed']) test('release Agent version selection: '+mode, () => {
  const dir = mkdtempSync(join(tmpdir(), 'sshguard-release-'));
  const git = (...args) => execFileSync('git', args, { cwd: dir, encoding: 'utf8' }).trim();
  try {
    for (const folder of ['internal/agent', 'internal/api', 'scripts', 'docs', 'docs/guides', 'docs/deployment', 'docs/development']) mkdirSync(join(dir, folder), { recursive: true });
    const files = {
      DOCS_VERSION: '0.4.0\n', VERSION: '0.3.1\n', 'internal/agent/run.go': 'var version = "0.3.1"\n',
      'internal/api/routes.go': 'var Version = "0.3.1"\nvar AgentVersion = "0.3.1"\n',
      'sshguard.sh': 'version=${SSHGUARD_VERSION:-v0.3.1}\n',
      'scripts/install-agent.sh': "RELEASE_VERSION='0.3.1'\n",
      'scripts/install-agent.ps1': "$ReleaseVersion = '0.3.1'\n",
      'README.md': 'SQLite 从 v0.3.1 开始支持。固定版本 `v0.3.1`\n',
      'CHANGELOG.md': '# 更新日志\n\n## 0.3.1\n\n- SQLite\n',
      'docs/guides/QUICKSTART.md': 'SSHGUARD_VERSION=v0.3.1 sh scripts/quick-start.sh\n',
      'docs/deployment/INSTALL-NATIVE.md': 'gh release download v0.3.1\n',
      'docs/development/RELEASING.md': 'git tag -a v0.3.1 -m "SSHGuard 0.3.1"\n',
      'docs/RELEASE-NOTES.md': '# SSHGuard v0.3.1\n',
    };
    if (mode !== 'legacy') files.AGENT_VERSION = '0.3.1\n';
    if (mode === 'changed') files['CHANGELOG.md'] = '# 更新日志\n\n## 0.3.2\n\n- 旧目录需要卸载后重新安装。\n\n## 0.3.1\n\n- SQLite\n';
    for (const [file, text] of Object.entries(files)) writeFileSync(join(dir, file), text);
    git('init', '-b', 'main');
    git('config', 'user.email', 'test@example.invalid');
    git('config', 'user.name', 'Test');
    git('add', '.'); git('commit', '-m', 'initial'); git('tag', 'v0.3.1');
    if (mode === 'changed') { writeFileSync(join(dir,'internal/agent/feature.go'),'package agent\n'); git('add','.');git('commit','-m','agent change'); }
    const unrelated = execFileSync('git', ['commit-tree', 'HEAD^{tree}'], { cwd: dir, input: 'unrelated branch\n', encoding: 'utf8' }).trim();
    git('tag', 'v9.0.0', unrelated);
    // Mock only the GitHub API response; ancestry/version/file operations use a real repository.
    const preload = join(dir, '.git/mock-gh.mjs');
    writeFileSync(preload, `import cp from 'node:child_process'; import {syncBuiltinESMExports} from 'node:module';
const real = cp.execFileSync; cp.execFileSync = (cmd, args, options) => cmd === 'gh' ? JSON.stringify([[{tag_name:'v0.3.1'},{tag_name:'v9.0.0'}]]) : real(cmd,args,options); syncBuiltinESMExports();`);
    const output = join(dir, '.git/output');
    const result = spawnSync(process.execPath, ['--import', pathToFileURL(preload).href, resolve('scripts/release/prepare-release.mjs')], {
      cwd: dir, encoding: 'utf8', env: { ...process.env, GITHUB_EVENT_NAME: 'workflow_dispatch', GITHUB_REF_TYPE: 'branch',
        GITHUB_REPOSITORY: 'example/repository', REQUESTED_VERSION: '', GITHUB_OUTPUT: output, GITHUB_STEP_SUMMARY: join(dir, '.git/summary') },
    });
    assert.equal(result.status, 0, result.stderr);
    assert.equal(readFileSync(join(dir, 'VERSION'), 'utf8'), '0.3.2\n');
    assert.equal(readFileSync(join(dir, 'DOCS_VERSION'), 'utf8'), '0.4.0\n');
    assert.match(readFileSync(output, 'utf8'), /tag=v0\.3\.2/);
    assert.match(readFileSync(join(dir, 'README.md'), 'utf8'), /SQLite 从 v0\.3\.1.*固定版本 `v0\.3\.2`/);
    assert.match(readFileSync(join(dir, 'internal/api/routes.go'), 'utf8'), /"0\.3\.2"/);
    assert.ok(readFileSync(join(dir, 'scripts/install-agent.sh'), 'utf8').includes("RELEASE_VERSION='"+(mode==='unchanged'?'0.3.1':'0.4.0')+"'"));
    assert.equal(readFileSync(join(dir,'AGENT_VERSION'),'utf8').trim(), mode==='unchanged'?'0.3.1':'0.4.0');
    assert.ok(readFileSync(join(dir, 'scripts/install-agent.ps1'), 'utf8').includes("$ReleaseVersion = '"+(mode==='unchanged'?'0.3.1':'0.4.0')+"'"));
    assert.match(readFileSync(join(dir, 'CHANGELOG.md'), 'utf8'), /## 0\.3\.2/);
    assert.match(readFileSync(join(dir, 'docs/RELEASE-NOTES.md'), 'utf8'), /^# SSHGuard v0\.3\.2/);
    if (mode === 'changed') assert.match(readFileSync(join(dir, 'docs/RELEASE-NOTES.md'), 'utf8'), /旧目录需要卸载后重新安装/);
    assert.equal(git('tag', '--list', 'v0.3.2'), '', 'preparation alone must not push or create tags');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
