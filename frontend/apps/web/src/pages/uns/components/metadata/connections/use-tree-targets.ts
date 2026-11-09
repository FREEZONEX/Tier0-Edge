import { useLayoutEffect, useState, type RefObject } from 'react';
import { visibleConnectAncestor } from '../../source-stream/connect-namespace';

type Point = { id: string; x: number; y: number };
type Layout = {
  height: number;
  targets: Record<string, Point>;
  sources: Record<string, Point>;
  streams: Record<string, { left: Point; right: Point }>;
};

// SaaS namespace-connections/use-tree-targets: normalized x, CSS-pixel y, live DOM anchors.
export function useTreeTargets(
  ref: RefObject<HTMLDivElement>,
  targetKeys: string[],
  parents: ReadonlyMap<string, string>
) {
  const [layout, setLayout] = useState<Layout>({ height: 1, targets: {}, sources: {}, streams: {} });
  useLayoutEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    let frame = 0;
    let previous = '';
    const measure = () => {
      const bounds = canvas.getBoundingClientRect();
      if (!bounds.width || !bounds.height) return;
      const x = (px: number) => ((px - bounds.left) / bounds.width) * 1000;
      const y = (rect: DOMRect) => rect.top + rect.height / 2 - bounds.top;
      const next: Layout = { height: bounds.height, sources: {}, streams: {}, targets: {} };
      const visible = (rect: DOMRect, viewport?: DOMRect) =>
        rect.width > 0 &&
        rect.height > 0 &&
        (!viewport || (rect.top >= viewport.top && rect.bottom <= viewport.bottom));
      const sourceViewport = canvas.querySelector('[data-source-viewport]')?.getBoundingClientRect();
      canvas.querySelectorAll<HTMLElement>('[data-source-anchor]').forEach((element) => {
        const rect = element.getBoundingClientRect();
        if (!visible(rect, sourceViewport)) return;
        const point = { id: element.dataset.sourceAnchor!, x: x(rect.right), y: y(rect) };
        element.dataset.sourceIds?.split(',').forEach((id) => {
          next.sources[id] = point;
        });
      });
      const streamViewport = canvas.querySelector('[data-stream-viewport]')?.getBoundingClientRect();
      canvas.querySelectorAll<HTMLElement>('[data-flow-stream]').forEach((element) => {
        const rect = element.getBoundingClientRect();
        if (!visible(rect, streamViewport)) return;
        const id = element.dataset.flowStream!;
        next.streams[id] = { left: { id, x: x(rect.left), y: y(rect) }, right: { id, x: x(rect.right), y: y(rect) } };
      });
      const tree = canvas.querySelector('[data-connect-namespace]');
      const viewport = tree?.getBoundingClientRect();
      const anchors = new Map<string, DOMRect>();
      tree?.querySelectorAll<HTMLElement>('[data-connect-anchor]').forEach((element) => {
        anchors.set(element.dataset.connectAnchor!, element.getBoundingClientRect());
      });
      const keys = new Set(anchors.keys());
      const searching = !!tree?.parentElement?.querySelector('input')?.value.trim();
      for (const key of targetKeys) {
        const id = searching ? key : visibleConnectAncestor(key, parents, keys);
        const rect = id && anchors.get(id);
        if (id && rect && visible(rect, viewport)) next.targets[key] = { id, x: x(rect.left - 8), y: y(rect) };
      }
      const serialized = JSON.stringify(next);
      if (serialized !== previous) {
        previous = serialized;
        setLayout(next);
      }
    };
    const schedule = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    };
    const resize = new ResizeObserver(schedule);
    resize.observe(canvas);
    const mutation = new MutationObserver(schedule);
    mutation.observe(canvas, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ['class', 'style', 'aria-expanded', 'data-source-ids'],
    });
    canvas.addEventListener('scroll', schedule, true);
    canvas.addEventListener('input', schedule, true);
    canvas.addEventListener('transitionend', schedule, true);
    measure();
    return () => {
      cancelAnimationFrame(frame);
      resize.disconnect();
      mutation.disconnect();
      canvas.removeEventListener('scroll', schedule, true);
      canvas.removeEventListener('input', schedule, true);
      canvas.removeEventListener('transitionend', schedule, true);
    };
  }, [ref, targetKeys, parents]);
  return layout;
}
