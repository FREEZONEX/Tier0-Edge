import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FC,
  type ReactNode,
} from 'react';
import { App } from 'antd';
import { useLocation, useNavigate } from 'react-router';
import { useTranslate } from '@/hooks';
import IntegrationNavigation from './IntegrationNavigation';
import { openConfirmModal } from '@/components/confirm-modal';
import { hasPermission } from '@/utils/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import { useBaseStore } from '@/stores/base';

export type IntegrationView = 'metadata' | 'namespace' | 'collections' | 'source' | 'stream' | 'edge';
export type IntegrationTarget = {
  view: IntegrationView;
  sourceId?: number;
  streamId?: number;
  namespaceId?: string | number;
  topicId?: number;
  tab?: string;
  create?: string;
  metaId?: string;
  section?: string;
  detail?: string;
  edit?: string;
  collectionId?: string;
  layout?: 'canvas' | 'list';
};

export const IntegrationRouteContext = createContext<
  { target: IntegrationTarget; go: (target: IntegrationTarget, replace?: boolean) => void } | undefined
>(undefined);
const RegistryRevisionContext = createContext({ revision: 0, changed: () => {} });
export const useRegistryRevision = () => useContext(RegistryRevisionContext);

const DraftContext = createContext<{ dirty: Set<string> } | undefined>(undefined);
export const IntegrationGuardProvider: FC<{ children: ReactNode }> = ({ children }) => {
  const [revision, setRevision] = useState(0);
  const changed = useCallback(() => setRevision((value) => value + 1), []);
  const registry = useMemo(() => ({ revision, changed }), [revision, changed]);
  const [state] = useState(() => ({ dirty: new Set<string>() }));
  const location = useLocation();
  const t = useTranslate();
  const currentIndex = useRef<number | undefined>(window.history.state?.idx);
  const restoring = useRef(false);
  useEffect(() => {
    currentIndex.current = window.history.state?.idx;
  }, [location.key]);
  useEffect(() => {
    const guard = (event: PopStateEvent) => {
      if (restoring.current) {
        restoring.current = false;
        event.stopImmediatePropagation();
        return;
      }
      const targetIndex: unknown = event.state?.idx;
      const previousIndex = currentIndex.current;
      if (!state.dirty.size || typeof targetIndex !== 'number' || typeof previousIndex !== 'number') return;
      // popstate cannot be cancelled. Stop the router notification and move
      // back to the same history entry, without adding or replacing entries.
      if (targetIndex !== previousIndex && !window.confirm(t('SourceStream.discardHint'))) {
        event.stopImmediatePropagation();
        restoring.current = true;
        window.history.go(previousIndex - targetIndex);
      }
    };
    window.addEventListener('popstate', guard, true);
    return () => window.removeEventListener('popstate', guard, true);
  }, [state, t]);
  return (
    <RegistryRevisionContext.Provider value={registry}>
      <DraftContext.Provider value={state}>{children}</DraftContext.Provider>
    </RegistryRevisionContext.Provider>
  );
};
export function useIntegrationDirty(key: string, dirty: boolean) {
  const state = useContext(DraftContext);
  useEffect(() => {
    if (dirty) state?.dirty.add(key);
    else state?.dirty.delete(key);
    return () => {
      state?.dirty.delete(key);
    };
  }, [state, key, dirty]);
  // 仅在实际保存成功后调用，避免关闭已保存弹窗时仍被旧的 dirty effect 拦截。
  return useCallback(() => {
    state?.dirty.delete(key);
  }, [state, key]);
}

export function useIntegrationLeave() {
  const draft = useContext(DraftContext);
  const { modal } = App.useApp();
  const t = useTranslate();
  return useCallback(
    (commit: () => void) => {
      if (draft?.dirty.size) {
        openConfirmModal(modal, {
          title: t('SourceStream.unsaved'),
          content: t('SourceStream.discardHint'),
          okText: t('SourceStream.discard'),
          cancelText: t('common.cancel'),
          onOk: commit,
        });
      } else commit();
    },
    [draft, modal, t]
  );
}

export function useIntegrationRoute() {
  const scoped = useContext(IntegrationRouteContext);
  const location = useLocation();
  const navigate = useNavigate();
  const leave = useIntegrationLeave();
  const params = useMemo(() => new URLSearchParams(location.search), [location.search]);
  useBaseStore((state) => state.buttonList);
  useBaseStore((state) => state.currentUserInfo);
  useBaseStore((state) => state.systemInfo?.authEnable);
  const rawView = params.get('view');
  const view: IntegrationView =
    rawView === 'source' || rawView === 'stream' || rawView === 'collections' || rawView === 'edge'
      ? rawView
      : location.pathname === '/connect'
        ? hasPermission(ButtonPermission['uns.sources'])
          ? 'source'
          : hasPermission(ButtonPermission['uns.streams'])
            ? 'stream'
            : 'edge'
        : 'namespace';
  const go = useCallback(
    (target: IntegrationTarget, replace = false) => {
      const next = new URLSearchParams();
      if (target.view === 'source' || target.view === 'stream') {
        if (params.get('metaId')) next.set('metaId', params.get('metaId')!);
        if (params.get('detail')) next.set('detail', params.get('detail')!);
      }
      Object.entries(target).forEach(([key, value]) => {
        if (value !== undefined && value !== '') next.set(key, String(value));
      });
      if (target.view === 'collections') next.set('view', 'record');
      const commit = () =>
        scoped
          ? scoped.go(target, replace)
          : navigate(
              {
                pathname:
                  target.view === 'collections'
                    ? '/collections'
                    : target.view === 'source' || target.view === 'stream' || target.view === 'edge'
                      ? '/connect'
                      : '/uns',
                search: `?${next}`,
              },
              { replace }
            );
      const sameObject = ['view', 'sourceId', 'streamId', 'create', 'metaId', 'section', 'edit'].every(
        (key) =>
          String(target[key as keyof IntegrationTarget] || '') ===
          String(scoped ? scoped.target[key as keyof IntegrationTarget] || '' : params.get(key) || '')
      );
      if (!replace && !sameObject) leave(commit);
      else commit();
    },
    [params, navigate, leave, scoped]
  );
  return {
    view,
    detail: params.get('detail') || 'data',
    metaId: params.get('metaId') || '',
    section: params.get('section') === 'collections' ? 'collections' : 'namespace',
    sourceId: Number(params.get('sourceId')) || 0,
    streamId: Number(params.get('streamId')) || 0,
    topicId: Number(params.get('topicId')) || 0,
    tab: params.get('tab') || 'overview',
    create: params.get('create'),
    layout: params.get('layout') === 'canvas' ? 'canvas' : 'list',
    edit: params.get('edit'),
    go,
    ...(scoped
      ? {
          ...scoped.target,
          sourceId: scoped.target.sourceId || 0,
          streamId: scoped.target.streamId || 0,
          create: scoped.target.create || null,
          edit: scoped.target.edit || null,
          tab: scoped.target.tab || 'overview',
          go,
        }
      : {}),
  };
}

const IntegrationTabs: FC = () => {
  const route = useIntegrationRoute();
  return <IntegrationNavigation {...route} />;
};

export default IntegrationTabs;
