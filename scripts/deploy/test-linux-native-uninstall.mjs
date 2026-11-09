import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {uninstallPlan,removeProgram} from '../../deploy/linux-native/uninstall.mjs';

function fixture(t) {
 const base=fs.mkdtempSync(path.join(os.tmpdir(),'tier0-uninstall-test-'));
 t.after(()=>fs.rmSync(base,{recursive:true,force:true}));
 const program=path.join(base,'program'),data=path.join(base,'data'),home=path.join(base,'home');
 for(const dir of [program,data,home])fs.mkdirSync(dir);
 const files={'tier0':'hash','native-launcher':'hash','node/node':'hash','backend/backend':'hash','web/asset.js':'hash','uninstall.mjs':'hash'};
 for(const name of Object.keys(files)){fs.mkdirSync(path.dirname(path.join(program,name)),{recursive:true});fs.writeFileSync(path.join(program,name),'program');}
 fs.writeFileSync(path.join(program,'.installation.json'),JSON.stringify({dataRoot:data}));
 const manifest={kind:'linux-native-edge',distribution:{edition:'edge',platform:'linux/amd64',deployment:'native'},files};
 const save=()=>fs.writeFileSync(path.join(program,'package.json'),JSON.stringify(manifest));save();
 return {program,data,home,manifest,save};
}

test('removes only inventory files and retains data, backups and unknown files',{skip:process.platform!=='linux'},t=>{
 const f=fixture(t);fs.mkdirSync(path.join(f.data,'backups'));fs.writeFileSync(path.join(f.data,'backups','snapshot'),'keep');fs.writeFileSync(path.join(f.program,'web','notes'),'keep');
 const plan=uninstallPlan(f.program,f.home);removeProgram(plan);
 assert.equal(fs.readFileSync(path.join(f.data,'backups','snapshot'),'utf8'),'keep');assert.equal(fs.readFileSync(path.join(f.program,'web','notes'),'utf8'),'keep');assert.equal(fs.existsSync(path.join(f.program,'native-launcher')),false);
});
test('rejects traversal, links, overlapping data and foreign desktop entries',{skip:process.platform!=='linux'},t=>{
 for(const name of ['../outside','web/../../outside','/tmp/outside','state/credentials','web\\..\\outside']){const f=fixture(t);f.manifest.files[name]='hash';f.save();assert.throws(()=>uninstallPlan(f.program,f.home));assert.ok(fs.existsSync(path.join(f.program,'tier0')));}
 const linked=fixture(t);fs.unlinkSync(path.join(linked.program,'web','asset.js'));fs.symlinkSync(linked.data,path.join(linked.program,'web','asset.js'));assert.throws(()=>uninstallPlan(linked.program,linked.home));
 const overlap=fixture(t);fs.writeFileSync(path.join(overlap.program,'.installation.json'),JSON.stringify({dataRoot:path.join(overlap.program,'data')}));assert.throws(()=>uninstallPlan(overlap.program,overlap.home));
 const desktop=fixture(t),plan=uninstallPlan(desktop.program,desktop.home);fs.mkdirSync(path.dirname(plan.desktop),{recursive:true});fs.writeFileSync(plan.desktop,'Exec="/other/tier0" start\n');assert.throws(()=>uninstallPlan(desktop.program,desktop.home));
});
test('requires this installation lock and refuses a competing holder',{skip:process.platform!=='linux'},t=>{
 const f=fixture(t),script=fileURLToPath(new URL('../../deploy/linux-native/uninstall.mjs',import.meta.url));
 fs.mkdirSync(path.join(f.data,'state'));const lock=path.join(f.data,'state','supervisor.lock');
 const owned=fs.openSync(lock,'w'),other=fs.openSync(lock,'r+');
 t.after(()=>{fs.closeSync(owned);fs.closeSync(other)});
 const stdio=fd=>{const a=Array(10).fill('ignore');a[1]='pipe';a[2]='pipe';a[9]=fd;return a;};
 assert.equal(spawnSync('flock',['-n','9'],{stdio:stdio(owned)}).status,0);
 const invoke=fd=>spawnSync(process.execPath,[script,'--remove',f.program],{stdio:stdio(fd),env:process.env});
 assert.notEqual(invoke(other).status,0);assert.ok(fs.existsSync(path.join(f.program,'tier0')));
 const removed=invoke(owned);assert.equal(removed.status,0,removed.stderr?.toString());assert.equal(fs.existsSync(path.join(f.program,'tier0')),false);assert.ok(fs.existsSync(lock));
});
