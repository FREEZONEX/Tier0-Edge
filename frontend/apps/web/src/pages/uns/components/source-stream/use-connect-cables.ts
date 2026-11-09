import { useEffect, useState, type RefObject } from 'react';
import type { ConnectRelation } from './connect-model';
import { visibleConnectAncestor } from './connect-namespace';

type Point = { x: number; y: number };
export type ConnectCable = { relation: ConnectRelation; path: string; anchor: string };

const curve = (start: Point, end: Point, read: boolean) => {
  const bend = Math.max(48, Math.abs(end.x - start.x) / 2) * (read ? -1 : 1);
  return `M ${start.x} ${start.y} C ${start.x + bend} ${start.y}, ${end.x - bend} ${end.y}, ${end.x} ${end.y}`;
};

// 使用实际屏幕坐标连接可缩放画布与固定 Namespace，不让树随 React Flow 的视口缩放。
export function useConnectCables(
  stage: RefObject<HTMLDivElement>,
  relations: ConnectRelation[],
  parentByKey: ReadonlyMap<string, string>
) {
  const [layout, setLayout] = useState({
    cables: [] as ConnectCable[],
    canvasClip: { x: 0, y: 0, width: 0, height: 0 },
  });
  useEffect(() => {
    const root = stage.current;
    if (!root) return;
    let frame = 0;
    const measure = () => {
      frame = 0;
      const bounds = root.getBoundingClientRect();
      const namespace = root.querySelector('[data-connect-namespace]')?.getBoundingClientRect();
      const canvas = root.querySelector('[data-connect-canvas]')?.getBoundingClientRect();
      const anchors = new Map<string, Point>();
      if (namespace && canvas) {
        root.querySelectorAll<HTMLElement>('[data-connect-anchor]').forEach((element) => {
          const key = element.dataset.connectAnchor!;
          const rect = element.getBoundingClientRect();
          const isStream = key.startsWith('stream:');
          const clip = isStream ? canvas : namespace;
          const x = isStream ? rect.left + rect.width / 2 : rect.left;
          const y = rect.top + rect.height / 2;
          if (
            rect.width > 0 &&
            rect.height > 0 &&
            x >= clip.left &&
            x <= clip.right &&
            y >= clip.top &&
            y <= clip.bottom
          )
            anchors.set(key, { x: x - bounds.left, y: y - bounds.top });
        });
      }
      const visible = new Set(anchors.keys());
      const next: ConnectCable[] = [];
      for (const relation of relations) {
        if (relation.kind === 'dependency') continue;
        const read = relation.kind === 'read';
        const resource = read ? relation.from : relation.to;
        const stream = (read ? relation.to : relation.from) + (read ? ':read' : ':write');
        const anchor = visibleConnectAncestor(resource, parentByKey, visible);
        const a = anchors.get(stream);
        const b = anchor && anchors.get(anchor);
        if (a && b) next.push({ relation, anchor, path: read ? curve(b, a, true) : curve(a, b, false) });
      }
      const nextLayout = {
        cables: next,
        canvasClip: canvas
          ? { x: canvas.left - bounds.left, y: canvas.top - bounds.top, width: canvas.width, height: canvas.height }
          : { x: 0, y: 0, width: 0, height: 0 },
      };
      setLayout((previous) => (JSON.stringify(previous) === JSON.stringify(nextLayout) ? previous : nextLayout));
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(measure);
    };
    const mutations = new MutationObserver(schedule);
    mutations.observe(root, { subtree: true, childList: true, attributes: true, attributeFilter: ['style', 'class'] });
    const resize = new ResizeObserver(schedule);
    resize.observe(root);
    root.addEventListener('scroll', schedule, true);
    window.addEventListener('resize', schedule);
    schedule();
    return () => {
      cancelAnimationFrame(frame);
      mutations.disconnect();
      resize.disconnect();
      root.removeEventListener('scroll', schedule, true);
      window.removeEventListener('resize', schedule);
    };
  }, [stage, relations, parentByKey]);
  return layout;
}
