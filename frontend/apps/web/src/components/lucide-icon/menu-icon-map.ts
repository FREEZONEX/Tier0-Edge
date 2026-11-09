import {
  Book,
  Box,
  CircleHelp,
  Clapperboard,
  Cctv,
  Files,
  GitFork,
  Home,
  LayoutGrid,
  ListTree,
  Play,
  Plug,
  Workflow,
  Zap,
  type LucideIcon,
} from 'lucide-react';
import type { ResourceProps } from '@/stores/types';
import { resolveLegacyLucideIcon } from './legacy-icon-map';

const resourceKeyIconMap: Record<string, LucideIcon> = {
  'home.page': Home,
  'uns.page': ListTree,
  'uns.connect.ui': Plug,
  'flow.collection.page': Workflow,
  'flow.event.page': Zap,
  'mqtt.auth.manage': GitFork,
  'project.view': LayoutGrid,
  'launchpad.view': Play,
  'cluster.edge.manage': Box,
  'notebook.view': Book,
  'anchor.model': Box,
  'anchor.scene': Clapperboard,
  'vision.camera.page': Cctv,
};

const urlIconMap: Record<string, LucideIcon> = {
  '/home': Home,
  '/uns': ListTree,
  '/uns/topology': Workflow,
  '/flow': Workflow,
  '/collection-flow': Workflow,
  '/event-flow': Workflow,
  '/edge-connection': GitFork,
  '/mqtt-auth': GitFork,
  '/project': LayoutGrid,
  '/launchpad': Play,
  '/cluster': Box,
  '/notebook': Book,
  '/anchor/model': Box,
  '/anchor/scene': Clapperboard,
  '/vision': Cctv,
};

const codeIconMap: Record<string, LucideIcon> = {
  home: Home,
  uns: ListTree,
  notebook: Book,
};

const fallbackIconMap: Record<string, LucideIcon> = {
  docs: Files,
  support: CircleHelp,
};

export const resolveMenuLucideIcon = (item: ResourceProps): LucideIcon => {
  const resourceKey = String(item.resourceKey || '');
  if (resourceKey && resourceKeyIconMap[resourceKey]) {
    return resourceKeyIconMap[resourceKey];
  }

  if (resourceKey.startsWith('home.')) return Home;
  if (resourceKey.startsWith('uns.')) return ListTree;
  if (resourceKey.includes('flow.collection') || resourceKey.startsWith('flow.collection.')) return Workflow;
  if (resourceKey.includes('flow.event') || resourceKey.startsWith('flow.event.')) return Zap;
  if (resourceKey.startsWith('flow.')) return Workflow;
  if (resourceKey.startsWith('mqtt.auth.')) return GitFork;
  if (resourceKey.startsWith('project.')) return LayoutGrid;
  if (resourceKey.startsWith('launchpad.')) return Play;
  if (resourceKey.startsWith('notebook.')) return Book;
  if (resourceKey.startsWith('cluster.')) return Box;
  if (resourceKey === 'anchor.model') return Box;
  if (resourceKey === 'anchor.scene') return Clapperboard;
  if (resourceKey.startsWith('vision.')) return Cctv;

  const url = String(item.url || '');
  if (url && urlIconMap[url]) {
    return urlIconMap[url];
  }

  const matchedUrl = Object.entries(urlIconMap).find(([path]) => url === path || url.startsWith(`${path}/`));
  if (matchedUrl) {
    return matchedUrl[1];
  }

  const code = String(item.code || '').toLowerCase();
  if (code && codeIconMap[code]) {
    return codeIconMap[code];
  }

  const iconName = String(item.icon || '').toLowerCase();
  for (const [key, Icon] of Object.entries(fallbackIconMap)) {
    if (iconName.includes(key)) {
      return Icon;
    }
  }

  if (item.icon) {
    return resolveLegacyLucideIcon(item.icon);
  }

  return ListTree;
};
