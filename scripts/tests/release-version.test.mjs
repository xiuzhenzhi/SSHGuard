import test from 'node:test';
import assert from 'node:assert/strict';
import { parseVersion, selectVersion } from '../release/release-version.mjs';

test('automatic patch increment never skips reserved tags', () => {
  assert.throws(() => selectVersion('', '0.3.1', ['v0.3.2', 'v1.0.0-rc.1'], ['v0.3.3']), /instead of skipping/);
  assert.equal(selectVersion('', '0.4.0', ['v0.3.1'], []), '0.4.1');
  assert.equal(selectVersion('', '0.3.1', [], []), '0.3.2');
});
test('explicit versions support a v prefix and prereleases but never overwrite tags', () => {
  assert.equal(selectVersion(' v0.4.0-rc.1 ', '0.3.1', [], []), '0.4.0-rc.1');
  assert.throws(() => selectVersion('0.3.1', '0.3.1', [], ['v0.3.1']), /already exists/);
});
test('invalid versions cannot become git refs or shell input', () => {
  for (const value of ['../main', '1.2', '01.2.3', '1.2.3-01', '1.2.3;echo x', '1.2.3\nmain', '1.2.3+build']) {
    assert.throws(() => parseVersion(value), value);
  }
});
