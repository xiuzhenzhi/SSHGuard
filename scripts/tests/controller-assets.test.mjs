import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,readdirSync,rmSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {createHash} from 'node:crypto';
import {stageControllerAssets,controllerAssets} from '../release/controller-assets.mjs';

test('public controller downloads exclude internal and duplicate Agent assets',()=>{
 const root=mkdtempSync(join(tmpdir(),'sshguard-assets-'));const source=join(root,'source'),output=join(root,'public');mkdirSync(source);
 try {
  const names=[...controllerAssets,'sshguard-agent-linux-amd64','VERSION','RELEASE-NOTES.md','deploy.sh'];
  for(const n of names)writeFileSync(join(source,n),n);
  const manifest=names.map(n=>`${createHash('sha256').update(n).digest('hex')}  ${n}`).join('\n')+'\n';
  writeFileSync(join(source,'SHA256SUMS'),manifest);
  stageControllerAssets(source,output);
  assert.deepEqual(readdirSync(output).sort(),[...controllerAssets,'SHA256SUMS'].sort());
  const lines=readFileSync(join(output,'SHA256SUMS'),'utf8').trim().split('\n');assert.equal(lines.length,3);
  for(const n of controllerAssets)assert.ok(lines.some(line=>line.endsWith('  '+n)));
  writeFileSync(join(source,controllerAssets[0]),'tampered');
  assert.throws(()=>stageControllerAssets(source,output),/checksum mismatch/);
  writeFileSync(join(source,controllerAssets[0]),controllerAssets[0]);
  writeFileSync(join(source,'SHA256SUMS'),manifest+manifest.split('\n')[0]+'\n');
  assert.throws(()=>stageControllerAssets(source,output),/checksum mismatch/);
 } finally {rmSync(root,{recursive:true,force:true});}
});

test('Docker builder matches the required Go toolchain',()=>{
 const goVersion=readFileSync(new URL('../../go.mod',import.meta.url),'utf8').match(/^go (\S+)$/m)?.[1];
 const dockerVersion=readFileSync(new URL('../../deploy/Dockerfile.api',import.meta.url),'utf8').match(/^FROM golang:([^ ]+)-alpine AS build$/m)?.[1];
 assert.ok(goVersion);assert.equal(dockerVersion,goVersion);
});
