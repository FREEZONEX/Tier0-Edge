import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import os from 'node:os';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';

function safePath(file) {
  for (let p = file; ; p = path.dirname(p)) {
    try { if (fs.lstatSync(p).isSymbolicLink()) throw Error('Refusing symlink: '+p); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
    if (path.dirname(p) === p) break;
  }
}

export function uninstallPlan(program, home = os.homedir()) {
  if (!path.isAbsolute(program) || path.normalize(program) !== program || program === '/' || program === home) throw Error('An absolute installation directory is required');
  safePath(program);
  for(const name of ['.installation.json','package.json']) safePath(path.join(program,name));
  const installation = JSON.parse(fs.readFileSync(path.join(program,'.installation.json'),'utf8'));
  const data = installation.dataRoot;
  if (typeof data !== 'string' || !path.isAbsolute(data) || path.normalize(data) !== data || data === '/' || data === home || data === program || data.startsWith(program+'/') || program.startsWith(data+'/')) throw Error('Program and data must be separate absolute directories');
  safePath(data);
  const manifest = JSON.parse(fs.readFileSync(path.join(program,'package.json'),'utf8'));
  if (manifest.kind !== 'linux-native-edge' || manifest.distribution?.edition !== 'edge' || manifest.distribution?.platform !== 'linux/amd64' || manifest.distribution?.deployment !== 'native' || !manifest.files || typeof manifest.files !== 'object' || Array.isArray(manifest.files)) throw Error('Not a Linux native Edge installation');
  const files = Object.keys(manifest.files);
  for (const required of ['tier0','native-launcher','node/node','backend/backend']) if (!files.includes(required)) throw Error('Incomplete program inventory');
  for (const name of files) {
    if (!name || path.isAbsolute(name) || path.normalize(name) !== name || name.startsWith('../') || name.includes('\\') || name.includes('\0') || ['data','state','logs','backups'].includes(name.split('/')[0])) throw Error('Unsafe program inventory path: '+name);
    const file = path.join(program,name); safePath(file);
    try { if (!fs.lstatSync(file).isFile()) throw Error('Program entry is not a file: '+name); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
  const id=crypto.createHash('sha256').update(program).digest('hex').slice(0,12);
  const desktop=path.join(home,'.local/share/applications','tier0-edge-'+id+'.desktop');
  safePath(desktop);
  const quote=s=>'"'+s.replace(/[\\"`$]/g,'\\$&').replace(/%/g,'%%')+'"';
  try {
    const content=fs.readFileSync(desktop,'utf8');
    if (!content.split('\n').includes('Exec='+quote(program+'/tier0')+' start')) throw Error('Desktop entry is not owned by this installation');
  } catch (error) { if (error.code !== 'ENOENT') throw error; }
  return {program,data,files,desktop};
}

export function removeProgram(plan) {
  const dirs = new Set();
  safePath(plan.desktop);
  try { fs.unlinkSync(plan.desktop); } catch(error) { if(error.code!=='ENOENT') throw error; }
  // Keep the control script, runtime and inventory until last for failure retry.
  const last = new Set(['tier0','uninstall.mjs','node/node']);
  const files = [...plan.files.filter(f=>!last.has(f)), ...plan.files.filter(f=>last.has(f)), '.installation.json','package.json'];
  for (const name of files) {
    const file=path.join(plan.program,name); safePath(file);
    try { fs.unlinkSync(file); } catch(error) { if(error.code!=='ENOENT') throw error; }
    for(let dir=path.dirname(file);dir!==plan.program;dir=path.dirname(dir)) dirs.add(dir);
  }
  for(const dir of [...dirs].sort((a,b)=>b.length-a.length).concat(plan.program)) {
    try { fs.rmdirSync(dir); } catch(error) { if(!['ENOENT','ENOTEMPTY','EEXIST'].includes(error.code)) throw error; }
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [mode,program]=process.argv.slice(2);
  if (!['--check','--remove'].includes(mode)) throw Error('Use tier0 uninstall [--yes]');
  const plan=uninstallPlan(program);
  if(mode==='--remove') {
    // The inherited descriptor must refer to this data root and own its flock.
    // An environment flag or another process holding the lock is not evidence.
    const fd=fs.fstatSync(9), lock=fs.statSync(path.join(plan.data,'state/supervisor.lock'));
    if(fd.dev!==lock.dev || fd.ino!==lock.ino) throw Error('Incorrect installation lock');
    const stdio=Array(10).fill('ignore');stdio[9]=9;
    if(spawnSync('flock',['-n','9'],{stdio}).status!==0) throw Error('Installation is still running');
    if(fs.existsSync(path.join(plan.data,'data/postgres/postmaster.pid'))) throw Error('PostgreSQL is not cleanly stopped');
    removeProgram(plan);
    console.log('Uninstalled program. Persistent data and backups retained: '+plan.data);
  } else console.log('Program: '+plan.program+'\nRetained data: '+plan.data);
}
