import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {spawnSync,execFileSync} from 'node:child_process';
import {attachDistribution} from './product-package.mjs';
const root=fs.realpathSync(process.argv[2]);
const files={};
function walk(dir){for(const e of fs.readdirSync(dir,{withFileTypes:true})){
 const file=path.join(dir,e.name);
 if(e.isDirectory()){walk(file);continue;}
 if(e.isSymbolicLink())throw Error('Resolve package symlinks before sealing: '+file);
 const bytes=fs.readFileSync(file),magic=bytes.subarray(0,4).toString('hex');
 if(magic==='7f454c46'){
  if(bytes[4]!==2||bytes[5]!==1||bytes.readUInt16LE(18)!==62)throw Error('Not Linux AMD64: '+file);
  const result=spawnSync('ldd',[file],{encoding:'utf8'}), output=(result.stdout||'')+(result.stderr||'');
  if(result.error||output.includes('not found')||(result.status!==0&&!/not a dynamic executable|statically linked/.test(output)))throw Error('Unresolved Linux shared libraries: '+file+' '+output);
 }
 if(['cffaedfe','cefaedfe','cafebabe','bebafeca'].includes(magic)||magic.startsWith('4d5a'))throw Error('Foreign executable: '+file);
 files[path.relative(root,file)]=crypto.createHash('sha256').update(bytes).digest('hex');
}}
walk(root);
for(const cli of ['npm/bin/npm-cli.js','pnpm/bin/pnpm.cjs'])execFileSync(path.join(root,'node/node'),[path.join(root,'node/node_modules',cli),'--version'],{stdio:'pipe',timeout:15000});
const manifest=attachDistribution(root,{schemaVersion:1,kind:'linux-native-edge',systemRequirements:{distribution:'Ubuntu 24.04',architecture:'amd64',glibc:'2.39',libraries:['libssl3t64','zlib1g','libstdc++6','libgcc-s1']},createdAt:new Date().toISOString(),postgresMajor:'17',dataFormat:1,supportedUpgradeFrom:[],files},'edge','linux/amd64','native');
if(manifest.sourceDirty)throw Error('Commit before sealing deliverable');
fs.writeFileSync(path.join(root,'package.json'),JSON.stringify(manifest,null,2)+'\n');
