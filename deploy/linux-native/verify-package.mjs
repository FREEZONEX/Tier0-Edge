import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {execFileSync} from 'node:child_process';
const root=fs.realpathSync(process.argv[2]);
const manifest=JSON.parse(fs.readFileSync(path.join(root,'package.json'),'utf8'));
if(manifest.distribution?.platform!=='linux/amd64'||manifest.distribution?.edition!=='edge'||manifest.distribution?.deployment!=='native')throw Error('Unsupported package identity');
if(!Object.keys(manifest.files||{}).length)throw Error('Missing checksums');
for(const [name,hash] of Object.entries(manifest.files)){
 const file=path.resolve(root,name);
 if(!file.startsWith(root+path.sep)||!fs.realpathSync(file).startsWith(root+path.sep))throw Error('Escaping package path');
 if(crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex')!==hash)throw Error('Checksum mismatch: '+name);
}
for(const name of ['native-launcher','backend/backend','backend/fleet-agent','backend/stream-executor','backend/source-modbus']){
 const id=JSON.parse(execFileSync(path.join(root,name),['--product-identity'],{encoding:'utf8',timeout:15000}));
 for(const key of ['edition','platform','deployment','productVersion','codeVersion'])if(id[key]!==manifest.distribution[key])throw Error('Compiled identity mismatch: '+name+' '+key);
}
console.log('Package integrity and compiled identity verified');
