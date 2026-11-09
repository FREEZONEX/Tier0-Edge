// Cloud #1042: 生命周期回执中的发布序号来自该次操作的不可变版本。
export function applyStreamReceipts(source) {
  const activeRelease = {type:'integer',minimum:1,description:'本次操作快照中的发布序号，可与运行记录的 release 比较。无活跃发布时省略；历史幂等回执未记录时不补查当前状态。'};
  for (const op of source.operations) {
    if (!['streamDocumentCreate','streamDocumentUpdate','streamDocumentGet','streamDocumentDisable','streamProgramDeploy','streamProgramRollback'].includes(op.name)) continue;
    const data=source.schemas[op.response]?.properties?.data;
    if (!data || data.type!=='object') throw new Error(`Stream receipt schema changed: ${op.name}`);
    data.properties={...(data.properties||{}),activeRelease:structuredClone(activeRelease)};
  }
  source.cloudContractRevision += '+stream-receipt/1';
}
