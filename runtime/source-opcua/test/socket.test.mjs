import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, rm, writeFile, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { start } from '../server.mjs';

test('managed socket rejects a live owner and regular files, and recovers after process death', { timeout: 10000 }, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'tier0-opcua-socket-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const socket = join(directory, 'runtime.sock');
  const child = spawn(process.execPath, ['--input-type=module', '-e',
    'import net from "node:net"; net.createServer().listen(process.argv[1], () => process.stdout.write("ready\\n"));', socket], { stdio: ['ignore', 'pipe', 'pipe'] });
  t.after(() => child.kill('SIGKILL'));
  await once(child.stdout, 'data');
  await assert.rejects(start(socket), /already running/);
  child.kill('SIGKILL');
  await once(child, 'exit');
  const runtime = await start(socket);
  t.after(() => new Promise((resolve) => { runtime.closeAllConnections(); runtime.close(resolve); }));
  await assert.rejects(start(socket), /already running/);
  const occupied = join(directory, 'occupied');
  await writeFile(occupied, 'preserve');
  await assert.rejects(start(occupied), /occupied/);
  assert.equal(await readFile(occupied, 'utf8'), 'preserve');
});
