import { mkdirSync, readFileSync, writeFileSync, copyFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';

export const controllerAssets = ['sshguard-linux-amd64.tar.gz','sshguard-linux-arm64.tar.gz','sshguard.sh'];
// CI keeps its full verification bundle. Public downloads contain only the
// controller packages and entry point, with a manifest matching those files.
export function stageControllerAssets(source, destination) {
 const manifest = readFileSync(join(source,'SHA256SUMS'),'utf8');
 const hashes = controllerAssets.map(name => {
  const hash = createHash('sha256').update(readFileSync(join(source,name))).digest('hex');
  const matches = manifest.split(/\r?\n/).map(line=>/^([a-f0-9]{64})\s+\*?(.+)$/.exec(line)).filter(m=>m?.[2]===name);
  if(matches.length!==1 || matches[0][1]!==hash) throw Error('Release checksum mismatch: '+name);
  return `${hash}  ${name}`;
 });
 mkdirSync(destination,{recursive:true});
 for(const name of controllerAssets)copyFileSync(join(source,name),join(destination,name));
 writeFileSync(join(destination,'SHA256SUMS'),hashes.join('\n')+'\n');
 return [...controllerAssets,'SHA256SUMS'];
}
if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href) {
 stageControllerAssets(resolve('dist/release'),resolve('dist/controller-release'));
}
