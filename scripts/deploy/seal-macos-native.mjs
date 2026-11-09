import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {attachDistribution} from './product-package.mjs';
const [root,app]=process.argv.slice(2);
const crossMac=process.platform==='linux';
function sign(file, bundle=false) {
  if (crossMac) execFileSync('rcodesign',['sign',...(bundle?['--shallow']:[]),file],{stdio:'pipe'});
  else execFileSync('codesign',['--force','--sign','-',file],{stdio:'pipe'});
}
function walk(dir){return fs.readdirSync(dir,{withFileTypes:true}).flatMap(e=>e.isDirectory()?walk(path.join(dir,e.name)):[path.join(dir,e.name)]);}
for(const file of walk(app)){
 if(fs.lstatSync(file).isSymbolicLink()){
  const resolved=fs.realpathSync(file);if(!resolved.startsWith(app+path.sep))throw Error('Escaping symlink: '+file);continue;
 }
 const fd=fs.openSync(file,'r'),head=Buffer.alloc(8);fs.readSync(fd,head,0,8,0);fs.closeSync(fd);
 const magic=head.subarray(0,4).toString('hex');
 if(magic.startsWith('4d5a')||magic==='7f454c46')throw Error('Foreign platform binary: '+file);
 if(['cffaedfe','cefaedfe','feedfacf','feedface','cafebabe','bebafeca','cafebabf','bfbafeca'].includes(magic)){
  if(magic!=='cffaedfe'||head.readUInt32LE(4)!==0x0100000c)throw Error('Not ARM64-only: '+file);
  sign(file);
 }
}
let manifest={schemaVersion:1,kind:'macos-native-edge',platform:'darwin',architecture:'arm64',productEdition:'edge',licenseActivationRequired:false,dockerRequired:false,sourceRevision:process.env.SOURCE_REVISION,createdAt:new Date().toISOString(),postgres:'17.11',timescale:'2.20.0',node:'22.23.1',nats:'2.15.2',pnpm:'10.13.1',signing:'ad-hoc; not notarized',files:{}};
manifest=attachDistribution(root,manifest,'edge','darwin/arm64','native',{crossMac});
if(manifest.sourceDirty)throw Error('Commit source changes before sealing a deliverable');
Object.assign(manifest,{postgresMajor:'17',dataFormat:1,supportedUpgradeFrom:['0.1.0','0.1.1','0.1.2','3.0.0']});
for(const file of walk(root)){if(!fs.lstatSync(file).isSymbolicLink())manifest.files[path.relative(root,file)]=crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');}
fs.writeFileSync(path.join(root,'package.json'),JSON.stringify(manifest,null,2)+'\n');
sign(app,true);
console.log('Sealed ARM64-only payload:',Object.keys(manifest.files).length,'files');
