#!/usr/bin/env node
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { resolveProduct, repo } from './product-package.mjs';

const options={};
for(let i=2;i<process.argv.length;i++) {
  const key=process.argv[i];
  if (key==='--help') { console.log('package --edition edge --platform windows/amd64|darwin/arm64|linux/amd64 --deployment native --cache DIR --output NEW_DIR [--web DIR] [--plan]');process.exit(0); }
  if(key==='--plan'){options.plan=true;continue;}
  if(!['--edition','--platform','--deployment','--cache','--output','--web','--pnpm','--binaries'].includes(key)||!process.argv[i+1]||process.argv[i+1].startsWith('--'))throw Error(`Invalid argument: ${key}`);
  options[key.slice(2)]=process.argv[++i];
}
const plan=resolveProduct(options.edition,options.platform,options.deployment);
if(options.plan){console.log(JSON.stringify(plan,null,2));process.exit(0);}
const host=`${process.platform==='win32'?'windows':process.platform}/${process.arch==='x64'?'amd64':process.arch}`;
const crossWindows=host==='linux/amd64' && plan.packageBackend==='windows-native';
const crossMac=host==='linux/amd64' && plan.packageBackend==='macos-native';
if(host!==plan.platform && !crossWindows && !crossMac)throw Error(`Build ${plan.platform} on its matching host; current host is ${host}`);
if(!options.cache||!options.output)throw Error('--cache and --output are required');
const run=(exe,args)=>{const r=spawnSync(exe,args,{stdio:'inherit',windowsHide:true});if(r.error)throw r.error;if(r.status!==0)throw Error(`${exe} failed (${r.status})`);};
if(plan.packageBackend==='windows-native'){
  const powershell=crossWindows?'pwsh':'pwsh.exe';
  const payload=path.resolve(options.output,'payload');
  const installerDir=path.resolve(options.output,crossWindows?'candidate':'release');
  run(powershell,['-NoProfile','-File',path.join(repo,'scripts/deploy/package-windows-native.ps1'),'-CacheRoot',options.cache,'-OutputRoot',payload,'-Edition',plan.edition,...(options.pnpm?['-PNPMRoot',options.pnpm]:[]),...(options.web?['-WebRoot',options.web]:[])]);
  run(powershell,['-NoProfile','-File',path.join(repo,'scripts/deploy/seal-windows-native.ps1'),'-PayloadRoot',payload,'-OutputDirectory',installerDir]);
} else if(plan.packageBackend==='macos-native') {
  run('bash',[path.join(repo,'scripts/deploy/package-macos-native.sh'),options.cache,options.output,options.web||path.join(repo,'frontend/apps/web/dist'),...(options.binaries?[options.binaries]:[])]);
} else if(plan.packageBackend==='linux-native') {
  run('bash',[path.join(repo,'scripts/deploy/package-linux-native.sh'),options.cache,options.output,options.web||path.join(repo,'frontend/apps/web/dist'),...(options.binaries?[options.binaries]:[])]);
} else throw Error('Package backend not implemented');
