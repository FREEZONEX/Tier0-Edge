import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
import { validateProductVersion, packageVersion } from './package-version.mjs';

export const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
export const catalog = JSON.parse(fs.readFileSync(path.join(repo, 'backend/internal/product/catalog.json'), 'utf8'));
export const productVersion = fs.readFileSync(path.join(repo, 'deploy/.env.default'), 'utf8').match(/^PRODUCT_VERSION=(.+)\r?$/m)[1].trim();
validateProductVersion(productVersion);

export function resolveProduct(edition, platform, deployment, requirePackager = true) {
  const override = catalog.editions[edition];
  if (!override) throw Error(`Unknown edition: ${edition}`);
  const target = catalog.targets[`${platform}/${deployment}`];
  if (!target || !target.editions.includes(edition)) throw Error(`Unsupported package combination: ${edition}/${platform}/${deployment}`);
  if (requirePackager && !target.packageBackend) throw Error(`Unified package backend not implemented: ${platform}/${deployment}. Legacy Docker packaging is not a validated unified product package.`);
  const active = new Set(), done = new Set(), components = [];
  function visit(id) {
    if (done.has(id)) return;
    if (active.has(id)) throw Error(`Component dependency cycle: ${id}`);
    const component = catalog.components[id];
    if (!component) throw Error(`Unknown component: ${id}`);
    active.add(id);
    component.dependencies.forEach(visit);
    active.delete(id); done.add(id); components.push(id);
  }
  [...catalog.base.components, ...(override.components || [])].forEach(visit);
  const capabilities = [...catalog.base.capabilities, ...override.capabilities].sort();
  return { schemaVersion: 1, productVersion, edition, platform, deployment, productName: override.name,
    activationRequired: override.activationRequired, adapters: {...catalog.base.adapters, ...override.adapters},
    components, capabilities, installationUnavailable: target.unavailable, packageBackend: target.packageBackend,
    dataCompatibility: { format: 1, postgresMajor: '17', metadata: 'metadata/1', data: 'data/3' } };
}

function binaryIdentity(run, file, expected) {
  // Runs a dedicated, side-effect-free query. Environment and manifests are not identity authorities.
  const actual = JSON.parse(run(file, '--product-identity'));
  for (const key of ['edition','platform','deployment','productVersion','codeVersion']) if (actual[key] !== expected[key]) throw Error(`Binary identity mismatch: ${key} (${actual[key]} != ${expected[key]})`);
}

// Platform adapters assemble files; this shared closure checks their output before sealing.
export function attachDistribution(root, manifest, edition, platform, deployment, { crossMac = false } = {}) {
  if (crossMac && (process.platform !== 'linux' || platform !== 'darwin/arm64')) throw Error('Cross-built versions require a Linux build host and a macOS ARM64 target');
  const plan = resolveProduct(edition, platform, deployment);
  const revision=execFileSync('git',['-C',repo,'rev-parse','HEAD'],{encoding:'utf8'}).trim();
  if (manifest.packageVersion && process.env.PACKAGE_VERSION && manifest.packageVersion !== process.env.PACKAGE_VERSION) throw Error('Conflicting package build identities');
  const buildVersion=packageVersion(productVersion, manifest.packageVersion || process.env.PACKAGE_VERSION);
  const identity={...plan,codeVersion:revision};
  const win = platform.startsWith('windows/'), ext = win ? '.exe' : '';
  const wine = win && process.platform === 'linux' ? process.env.TIER0_WINDOWS_WINE || 'wine' : '';
  const run = (file, arg) => execFileSync(wine || file, wine ? [path.resolve(file), arg] : [arg], {
    encoding: 'utf8', timeout: wine ? 120000 : 15000, windowsHide: true,
  }).trim();
  const artifacts = {
    backend:`backend/backend${ext}`, fleet:`backend/fleet-agent${ext}`, executor:`backend/stream-executor${ext}`,
    modbus:`backend/source-modbus${ext}`, web:'web/index.html', opcua:'opcua/package.json', mqtt:'mqtt/package.json',
    node:`node/node${ext}`, nats:`nats/nats-server${ext}`, postgres:`postgres/bin/postgres${ext}`,
    timescale:win?'postgres/share/extension/timescaledb.control':'postgres/share/postgresql/extension/timescaledb.control',
  };
  for (const id of plan.components) if (!artifacts[id] || !fs.statSync(path.join(root,artifacts[id])).isFile()) throw Error(`Missing component artifact: ${id}`);
  // Backends may not silently retain a component removed from the shared profile.
  for (const [id, artifact] of Object.entries(artifacts)) if (!plan.components.includes(id) && fs.existsSync(path.join(root,artifact))) throw Error(`Component is not declared by the product profile: ${id}`);
  for (const retired of ['redis','garnet','dotnet','rustfs','anchor','vision','notebook','ai-agent']) if (fs.existsSync(path.join(root,retired))) throw Error(`Unexpected retired component in package: ${retired}`);
  if (!crossMac) {
    for (const id of ['backend','fleet','executor','modbus']) binaryIdentity(run,path.join(root,artifacts[id]),identity);
    binaryIdentity(run,path.join(root,win?'Start.exe':'native-launcher'),identity);
    if (win) binaryIdentity(run,path.join(root,'Uninstall.exe'),identity);
  }
  const versions = {
    backend: productVersion, web:productVersion, fleet:productVersion, executor:productVersion, modbus:productVersion,
    node: crossMac ? manifest.node : run(path.join(root,artifacts.node), '--version').replace(/^v/,''),
    postgres: crossMac ? `postgres (PostgreSQL) ${manifest.postgres} (Postgres.app)` : run(path.join(root,artifacts.postgres), '--version'),
    timescale:fs.readFileSync(path.join(root,artifacts.timescale),'utf8').match(/default_version\s*=\s*'([^']+)'/)?.[1],
    nats: crossMac ? `nats-server: v${manifest.nats}` : run(path.join(root,artifacts.nats), '--version'),
    opcua:JSON.parse(fs.readFileSync(path.join(root,artifacts.opcua),'utf8')).version,
    mqtt:productVersion,
  };
  for (const id of plan.components) if (!versions[id]) throw Error(`Missing component version: ${id}`);
  const dirty=Boolean(execFileSync('git',['-C',repo,'status','--porcelain'],{encoding:'utf8'}).trim());
  return {...manifest, packageVersion:buildVersion, productEdition:edition, licenseActivationRequired:plan.activationRequired, productVersion,
    sourceRevision:revision, sourceDirty:dirty,
    distribution:{...plan,codeVersion:revision,components:plan.components.map(id=>({id,version:versions[id],artifact:artifacts[id],...catalog.components[id]}))}};
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [action,edition,platform,deployment,root] = process.argv.slice(2);
  if (action === 'build-version') console.log(packageVersion(productVersion, process.env.PACKAGE_VERSION));
  else if (action === 'plan') console.log(JSON.stringify(resolveProduct(edition,platform,deployment)));
  else if (action === 'manifest') {
    const file=path.join(root,'package.json');
    fs.writeFileSync(file, JSON.stringify(attachDistribution(root,JSON.parse(fs.readFileSync(file,'utf8')),edition,platform,deployment),null,2)+'\n');
  } else throw Error('Usage: product-package.mjs plan|manifest EDITION PLATFORM DEPLOYMENT [PAYLOAD]');
}
