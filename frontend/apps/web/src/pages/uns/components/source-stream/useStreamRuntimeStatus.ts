import { useSourceStreamApi } from '@/apis/core-api/source-stream-scope';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { type Stream } from '@/apis/core-api/source-stream';
import { CustomAxiosConfigEnum } from '@/utils/request';

const POLL_INTERVAL_MS = 2000;
const POLL_WINDOW_MS = 60000;
type RefreshProblem = 'failed' | 'expired' | 'changed';
type Observation = { key: string; refreshing: boolean; problem?: RefreshProblem };
const recovering = (state?: Stream['executionState']) => state === 'starting' || state === 'source-watch-failed';

/** 观测运行状态或同一候选版本的确认激活；不覆盖文档，也不触发部署。 */
export function useStreamRuntimeStatus(
  stream: Stream | undefined,
  enabled: boolean,
  onObserved: (value: Stream) => void
) {
  const sourceStreamApi = useSourceStreamApi();
  const key = JSON.stringify([
    stream?.id,
    stream?.revision,
    stream?.activeVersion,
    stream?.pendingVersion,
    stream?.status,
    enabled,
  ]);
  const [observation, setObservation] = useState<Observation>({ key, refreshing: false });
  const current = useRef({ stream, enabled, onObserved });
  useLayoutEffect(() => {
    current.current = { stream, enabled, onObserved };
  }, [stream, enabled, onObserved]);
  const refreshRef = useRef<() => void>(() => {});
  const id = stream?.id;
  const revision = stream?.revision;
  const activeVersion = stream?.activeVersion;
  const pendingVersion = stream?.pendingVersion;
  const status = stream?.status;

  useEffect(() => {
    let disposed = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let deadline: ReturnType<typeof setTimeout> | undefined;
    let request: AbortController | undefined;
    let campaign = 0;
    const update = (patch: Partial<Omit<Observation, 'key'>>) =>
      setObservation((previous) => ({
        ...(previous.key === key ? previous : { key, refreshing: false }),
        ...patch,
      }));
    const setProblem = (problem?: RefreshProblem) => update({ problem });
    const setRefreshing = (refreshing: boolean) => update({ refreshing });
    const stop = () => {
      clearTimeout(timer);
      clearTimeout(deadline);
      request?.abort();
      campaign++;
    };
    const eligible = () => {
      const latest = current.current;
      return (
        !disposed &&
        latest.enabled &&
        !!latest.stream &&
        latest.stream.id === id &&
        latest.stream.revision === revision &&
        latest.stream.activeVersion === activeVersion &&
        (latest.stream.status === 'enabled' || !!latest.stream.pendingVersion)
      );
    };
    const poll = async (generation: number) => {
      if (!eligible() || campaign !== generation) return;
      request = new AbortController();
      const signal = request.signal;
      setProblem(undefined);
      setRefreshing(true);
      try {
        const observed = await sourceStreamApi.stream(id!, {
          signal,
          timeout: 5000,
          [CustomAxiosConfigEnum.NoMessage]: true,
        });
        if (signal.aborted || !eligible() || campaign !== generation) return;
        const activatedCandidate =
          !!pendingVersion &&
          (observed.pendingVersion ?? 0) === 0 &&
          observed.activeVersion === pendingVersion &&
          observed.status === 'enabled' &&
          observed.revision === (revision ?? 0) + 1 &&
          JSON.stringify(observed.document) === JSON.stringify(current.current.stream?.document) &&
          JSON.stringify(observed.draftManifest) === JSON.stringify(current.current.stream?.draftManifest);
        if (
          observed.id !== id ||
          (!activatedCandidate && observed.revision !== revision) ||
          (!activatedCandidate &&
            (observed.activeVersion !== activeVersion ||
              observed.status !== status ||
              (observed.pendingVersion ?? 0) !== (pendingVersion ?? 0)))
        ) {
          stop();
          setRefreshing(false);
          setProblem('changed');
          return;
        }
        const latest = current.current.stream!;
        current.current.onObserved({
          ...latest,
          executionState: observed.executionState,
          ...(activatedCandidate
            ? {
                activeVersion: observed.activeVersion,
                pendingVersion: observed.pendingVersion,
                revision: observed.revision,
                status: observed.status,
                manifest: observed.manifest,
                pendingManifest: observed.pendingManifest,
              }
            : {}),
        });
        if (observed.pendingVersion || recovering(observed.executionState))
          timer = setTimeout(() => void poll(generation), POLL_INTERVAL_MS);
        else clearTimeout(deadline);
      } catch {
        if (!signal.aborted && eligible() && campaign === generation) {
          stop();
          setRefreshing(false);
          setProblem('failed');
        }
      } finally {
        if (!disposed && campaign === generation) setRefreshing(false);
      }
    };
    const begin = (immediate: boolean) => {
      if (!eligible()) return;
      stop();
      const generation = campaign;
      deadline = setTimeout(() => {
        if (!eligible() || campaign !== generation) return;
        stop();
        setRefreshing(false);
        setProblem('expired');
      }, POLL_WINDOW_MS);
      if (immediate) void poll(generation);
      else timer = setTimeout(() => void poll(generation), POLL_INTERVAL_MS);
    };
    refreshRef.current = () => begin(true);
    if (
      enabled &&
      (status === 'enabled' || !!pendingVersion) &&
      (pendingVersion || recovering(current.current.stream?.executionState))
    )
      begin(false);
    return () => {
      disposed = true;
      stop();
      refreshRef.current = () => {};
    };
  }, [id, revision, activeVersion, pendingVersion, status, enabled, key]);

  return {
    refreshing: observation.key === key && observation.refreshing,
    problem: observation.key === key ? observation.problem : undefined,
    refresh: () => refreshRef.current(),
  };
}
