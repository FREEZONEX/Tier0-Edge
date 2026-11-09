import { createContext, useContext } from 'react';
import { sourceStreamApi } from './source-stream';
import { loadMetadata, metadataApi } from './uns-metadata';

export const SourceStreamScopeContext = createContext({
  readOnly: false,
  api: sourceStreamApi,
  nodeId: undefined as string | undefined,
  loadMetadata,
  findMetadata: metadataApi.find,
});
export const useSourceStreamScope = () => useContext(SourceStreamScopeContext);
export const useSourceStreamApi = () => useSourceStreamScope().api;
