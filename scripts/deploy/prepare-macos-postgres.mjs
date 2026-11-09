import fs from 'node:fs';
import path from 'node:path';
import {execFileSync} from 'node:child_process';
const [source,target]=process.argv.slice(2);
if (!source || !target || fs.existsSync(target)) throw Error('New PostgreSQL destination required');
const copied=new Set();
function copy(relative) {
  if(copied.has(relative))return;
  if(relative.startsWith('..')||path.isAbsolute(relative))throw Error('Dependency escapes PostgreSQL bundle');
  copied.add(relative);
  const from=path.join(source,relative),to=path.join(target,relative);
  fs.mkdirSync(path.dirname(to),{recursive:true});
  fs.copyFileSync(from,to);fs.chmodSync(to,0o755);
  const arch=execFileSync('lipo',['-archs',to],{encoding:'utf8'}).trim();
  if(!arch.split(' ').includes('arm64'))throw Error('ARM64 missing: '+relative);
  if(arch!=='arm64'){execFileSync('lipo',[to,'-thin','arm64','-output',to+'.arm64']);fs.renameSync(to+'.arm64',to);}
  const libs=execFileSync('otool',['-L',to],{encoding:'utf8'}).split('\n').slice(1).map(l=>l.trim().split(' (')[0]).filter(Boolean);
  for(const lib of libs){
    if(lib.startsWith('/usr/lib/')||lib.startsWith('/System/Library/'))continue;
    let rel;
    if(lib.startsWith('@loader_path/'))rel=path.normalize(path.join(path.dirname(relative),lib.slice(13)));
    else if(lib.includes('/Contents/Versions/17/'))rel=lib.split('/Contents/Versions/17/')[1];
    else if(lib.startsWith('@rpath/'))rel='lib/'+path.basename(lib);
    else throw Error('External dependency: '+lib);
    if(rel===relative)continue;
    copy(rel);
    const relocated='@loader_path/'+path.relative(path.dirname(to),path.join(target,rel));
    if(relocated!==lib)execFileSync('install_name_tool',['-change',lib,relocated,to]);
  }
  execFileSync('codesign',['--force','--sign','-',to],{stdio:'pipe'});
}
for(const name of ['postgres','initdb','pg_ctl','psql','createdb','pg_isready'])copy('bin/'+name);
for(const name of fs.readdirSync(path.join(source,'lib/postgresql'))){
  if(/^(timescaledb.*|pg_stat_statements|plpgsql|dict_snowball)\.dylib$/.test(name))copy('lib/postgresql/'+name);
}
const share=path.join(source,'share/postgresql');
fs.cpSync(share,path.join(target,'share/postgresql'),{recursive:true,dereference:true,filter:from=>{
 const relative=path.relative(share,from);
 return !relative.startsWith('extension/') || /^(timescaledb|pg_stat_statements|plpgsql)(--|\.)/.test(path.basename(from));
}});
// Copy only installed extensions, including their control and versioned SQL files.
console.log('PostgreSQL ARM64 Mach-O dependency closure:',copied.size);
