// Pure version selection, shared by the release preparation script and tests.
export function parseVersion(value) {
  const version = value.trim().replace(/^v/, '');
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/.exec(version);
  if (!match || version.length > 32 || match.slice(1, 4).some(n => !Number.isSafeInteger(Number(n))) ||
      (match[4] && match[4].split('.').some(n => /^\d+$/.test(n) && n.length > 1 && n.startsWith('0')))) {
    throw Error('Use a version such as 0.3.2 or 0.4.0-rc.1 (maximum 32 characters).');
  }
  return { version, numbers: match.slice(1, 4).map(Number), prerelease: !!match[4] };
}

function compare(a, b) {
  for (let i = 0; i < 3; i++) if (a.numbers[i] !== b.numbers[i]) return a.numbers[i] - b.numbers[i];
  return 0;
}

export function selectVersion(requested, current, released, tags) {
  const existing = new Set(tags);
  if (requested.trim()) {
    const chosen = parseVersion(requested);
    if (existing.has('v' + chosen.version)) throw Error('Version tag already exists; never overwrite a published version.');
    return chosen.version;
  }
  let base = parseVersion(current);
  for (const tag of released) {
    let candidate;
    try { candidate = parseVersion(tag); } catch { continue; }
    if (!candidate.prerelease && compare(candidate, base) > 0) base = candidate;
  }
  let [major, minor, patch] = base.numbers;
  const version = `${major}.${minor}.${patch + 1}`;
  parseVersion(version);
  if (existing.has('v' + version)) throw Error('Next version tag already exists; retry that release instead of skipping a version.');
  return version;
}
