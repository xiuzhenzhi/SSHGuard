import { execFileSync, spawnSync } from 'node:child_process';
import { appendFileSync, readFileSync, writeFileSync, existsSync } from 'node:fs';
import { agentChanged, agentTag } from './agent-version.mjs';
import { parseVersion, selectVersion } from './release-version.mjs';

const git = (...args) => execFileSync('git', args, { encoding: 'utf8' }).trim();
if (process.env.GITHUB_EVENT_NAME !== 'workflow_dispatch' || process.env.GITHUB_REF_TYPE !== 'branch') {
  throw Error('Manual version preparation requires Run workflow on a branch.');
}
if (git('status', '--porcelain')) throw Error('Release preparation requires a clean checkout.');
const repository = process.env.GITHUB_REPOSITORY;
if (!/^[\w.-]+\/[\w.-]+$/.test(repository || '')) throw Error('Invalid repository.');
const current = parseVersion(readFileSync('VERSION', 'utf8')).version;
// Only published stable releases whose tagged commits belong to this branch
// influence automatic versioning. An unrelated branch cannot raise the version.
const pages = JSON.parse(execFileSync('gh', ['api', '--paginate', '--slurp', `repos/${repository}/releases?per_page=100`], { encoding: 'utf8' }));
const releases = pages.flat().filter(r => !r.draft && !r.prerelease);
const reachable = releases.filter(r => {
  try { parseVersion(r.tag_name); } catch { return false; }
  return spawnSync('git', ['merge-base', '--is-ancestor', `refs/tags/${r.tag_name}^{commit}`, 'HEAD']).status === 0;
}).map(r => r.tag_name);
const tags = git('tag', '--list').split('\n');
if (!process.env.REQUESTED_VERSION?.trim() && tags.includes('v' + current) &&
    !releases.some(r => r.tag_name === 'v' + current)) {
  throw Error('Current version has an unfinished release; retry its existing_tag instead of skipping a version.');
}
const version = selectVersion(process.env.REQUESTED_VERSION || '', current, reachable, tags);
const tag = 'v' + version;
if (releases.some(r => r.tag_name === tag)) throw Error('A Release already uses this version.');

const oldAgent = existsSync('AGENT_VERSION') ? readFileSync('AGENT_VERSION','utf8').trim() : current;
const changed = !existsSync('AGENT_VERSION') || agentChanged(oldAgent);
const [agentMajor, agentMinor, agentPatch] = parseVersion(oldAgent).numbers;
const agentVersion = changed ? `${agentMajor}.${agentMinor}.${agentPatch + 1}` : oldAgent;
if (changed && tags.includes(agentTag(agentVersion))) throw Error('Next Agent tag already exists; retry that release instead of skipping a version.');
writeFileSync('AGENT_VERSION', agentVersion + '\n');
writeFileSync('VERSION', version + '\n');
for (const [file, pattern, replacement] of [
  ['internal/agent/run.go', /var version = "[^"]+"/, `var version = "${agentVersion}"`],
  ['internal/api/routes.go', /var Version = "[^"]+"/, `var Version = "${version}"`],
  ['internal/api/routes.go', /var AgentVersion = "[^"]+"/, `var AgentVersion = "${agentVersion}"`],
  ['sshguard.sh', /SSHGUARD_VERSION:-v[^}]+/, `SSHGUARD_VERSION:-${tag}`],
  ['scripts/install-agent.sh', /RELEASE_VERSION='[^']+'/, `RELEASE_VERSION='${agentVersion}'`],
  ['scripts/install-agent.ps1', /\$ReleaseVersion = '[^']+'/, `$$ReleaseVersion = '${agentVersion}'`],
]) {
  const source = readFileSync(file, 'utf8');
  if (!pattern.test(source)) throw Error('Missing version declaration: ' + file);
  writeFileSync(file, source.replace(pattern, replacement));
}
// Update installation examples while keeping historical feature version claims.
for (const file of ['README.md', 'docs/guides/QUICKSTART.md', 'docs/deployment/INSTALL-NATIVE.md', 'docs/development/RELEASING.md']) {
  let text = readFileSync(file, 'utf8');
  text = text.replace(/(固定版本 `)v[0-9][^`]*(`)/g, `$1${tag}$2`)
    .replace(/(gh release download |SSHGUARD_VERSION=|git tag -a |git push origin )v\d+\.\d+\.\d+(?:-[\w.-]+)?/g, `$1${tag}`)
    .replace(/(-m "SSHGuard )\d+\.\d+\.\d+(?:-[\w.-]+)?(")/g, `$1${version}$2`);
  writeFileSync(file, text);
}
const previous = reachable.find(t => t === 'v' + current);
const range = previous ? `${previous}..HEAD` : 'HEAD';
const subjects = git('log', range, '-50', '--no-merges', '--format=%s').split('\n').filter(Boolean);
let changes = subjects.length ? subjects.map(s => '- ' + s).join('\n') : '- 基于所选分支构建并发布。';
let changelog = readFileSync('CHANGELOG.md', 'utf8');
// Preserve curated release instructions, especially breaking installation changes.
const section = changelog.replaceAll('\r\n', '\n').split(`\n## ${version}\n`)[1];
if (section) changes = section.split('\n## ')[0].trim();
if (!changelog.includes(`\n## ${version}\n`)) {
  changelog = changelog.replace(/^(# [^\n]+\n)/, `$1\n## ${version}\n\n${changes}\n`);
  writeFileSync('CHANGELOG.md', changelog);
}
writeFileSync('docs/RELEASE-NOTES.md', `# SSHGuard ${tag}\n\n${changes}\n\n升级前备份数据库、配置和 ENCRYPTION_KEY。数据库类型不会自动切换或搬迁，程序回退不撤销数据库迁移。安装与升级见 [快速开始](guides/QUICKSTART.md)。\n`);
appendFileSync(process.env.GITHUB_OUTPUT, `version=${version}\ntag=${tag}\n`);
appendFileSync(process.env.GITHUB_STEP_SUMMARY, `## Release preparation\n\nSelected version: **${tag}**\n\nMode: ${process.env.REQUESTED_VERSION?.trim() ? 'explicit version' : 'automatic patch increment'}\n`);
console.log('Prepared ' + tag);
