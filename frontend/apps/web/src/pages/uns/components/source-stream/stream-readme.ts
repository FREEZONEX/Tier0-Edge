import type { ProgramDocument, ProgramEntry, ProgramManifest } from '@/apis/core-api/source-stream';
import { compileWorkflow, readWorkflow } from './program-workflow';

// 未校验草稿的 Go slice 可能是 null；只规范化展示，不修改保存的脚本或 manifest。
export function readmeManifest(manifest?: ProgramManifest | null): ProgramManifest | undefined {
  if (!manifest) return undefined;
  return {
    ...manifest,
    sources: (manifest.sources ?? []).map((source) => ({ ...source, resources: source.resources ?? [] })),
    topics: manifest.topics ?? [],
    entries: manifest.entries ?? [],
  };
}

export function readmeDescription(document?: ProgramDocument): string {
  return typeof document?.description === 'string' ? document.description : '';
}

// Agent 可直接修改脚本。旧步骤只在与保存脚本完全一致时用于说明，绝不回写脚本。
export function documentedWorkflow(document: ProgramDocument) {
  const workflow = readWorkflow(document);
  if (!workflow) return;
  try {
    return compileWorkflow(workflow) === document.script ? workflow : undefined;
  } catch {
    return;
  }
}

export function entryMode(kind: ProgramEntry['kind']) {
  if (kind === 'http-request') return 'http';
  if (kind === 'interval') return 'interval';
  if (kind === 'source-event' || kind === 'uns-event') return 'message';
  return 'request';
}
