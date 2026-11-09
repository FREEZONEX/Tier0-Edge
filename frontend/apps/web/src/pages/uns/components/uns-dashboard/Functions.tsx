import { useEffect, useMemo, useState } from 'react';
import { Flex, Segmented } from 'antd';
import ComEllipsis from '@/components/com-ellipsis';
import useTranslate from '@/hooks/useTranslate.ts';
import { fetchBaseStore, useBaseStore } from '@/stores/base';
import ProCard from '@/components/pro-card/ProCard.tsx';
import { MenuLucideIcon } from '@/components/lucide-icon';
import type { ResourceProps } from '@/stores/types.ts';
import { useMenuNavigate } from '@/hooks';
import styles from './index.module.scss';
import { useActivate } from '@/contexts/tabs-lifecycle-context.ts';

const functionGroupKeys = new Set(['uns', 'system']);
const namespaceFunctionKeys = new Set(['uns.page', 'mqtt.auth.manage']);
const namespaceFunctionUrls = new Set(['/uns', '/edge-connection']);

const isNamespaceFunction = (item?: ResourceProps) =>
  namespaceFunctionKeys.has(String(item?.resourceKey || '')) || namespaceFunctionUrls.has(String(item?.url || ''));

const resourceIdentity = (item: ResourceProps) => String(item.resourceKey || item.url || item.id);

const uniqueResources = (items: ResourceProps[]) => {
  const seen = new Set<string>();
  return items.reduce<ResourceProps[]>((list, item) => {
    const key = resourceIdentity(item);
    if (seen.has(key)) return list;
    seen.add(key);
    list.push(item);
    return list;
  }, []);
};

const flattenResourceTree = (items: ResourceProps[] = []) => {
  const out: ResourceProps[] = [];
  const walk = (nodes: ResourceProps[]) => {
    nodes.forEach((node) => {
      out.push(node);
      if (node.children?.length) {
        walk(node.children);
      }
    });
  };
  walk(items);
  return out;
};

const toFunctionGroup = (item: ResourceProps) => ({
  ...item,
  children: item.children?.length ? item.children : [item],
});

const isFunctionGroup = (item: ResourceProps) =>
  item.children?.length && functionGroupKeys.has(String(item.resourceKey || ''));

const buildFunctionGroups = (menuTree: ResourceProps[] = [], homeTree: ResourceProps[] = []) => {
  const menuGroups = menuTree.filter(isFunctionGroup);
  const homeGroups = homeTree.filter(isFunctionGroup);
  const rawGroups = (menuGroups.length ? menuGroups : homeGroups).map(toFunctionGroup);
  const sourceNodes = flattenResourceTree([...menuTree, ...homeTree]);
  const namespaceItems = uniqueResources(sourceNodes.filter(isNamespaceFunction)).sort(
    (a, b) => Number(a.sort || 0) - Number(b.sort || 0)
  );
  const namespaceBase =
    namespaceItems.find((item) => item.resourceKey === 'uns.page' || item.url === '/uns') ||
    rawGroups.find((item) => item.resourceKey === 'uns') ||
    namespaceItems[0];
  if (!namespaceBase || !namespaceItems.length) {
    return rawGroups;
  }

  const namespaceGroup: ResourceProps = {
    ...namespaceBase,
    id: `${namespaceBase.id}:functions`,
    children: namespaceItems,
  };
  const result: ResourceProps[] = [];
  let namespaceInserted = false;

  rawGroups.forEach((group) => {
    const children = uniqueResources(
      (group.children?.length ? group.children : [group]).filter((item) => !isNamespaceFunction(item))
    );
    const shouldInsertNamespace =
      !namespaceInserted &&
      (group.resourceKey === 'uns' || isNamespaceFunction(group) || group.children?.some(isNamespaceFunction));
    if (shouldInsertNamespace) {
      result.push(namespaceGroup);
      namespaceInserted = true;
    }
    if (children.length) {
      result.push({
        ...group,
        children,
      });
    }
  });

  if (!namespaceInserted) {
    result.unshift(namespaceGroup);
  }
  return result;
};

const Functions = () => {
  useActivate(() => {
    fetchBaseStore?.();
  });
  const formatMessage = useTranslate();
  const { homeTree, menuTree } = useBaseStore((state) => ({
    homeTree: state.homeTree,
    menuTree: state.menuTree,
  }));

  const translateResourceText = (value?: string) => {
    if (!value) return '';
    return formatMessage(value);
  };

  const list = useMemo(() => {
    return buildFunctionGroups(menuTree || [], homeTree || []);
  }, [homeTree, menuTree]);
  const handleNavigate = useMenuNavigate();
  const defaultGroupId = useMemo(() => {
    const systemGroup = list.find((item) => {
      const name = String(item.showName || '').toLowerCase();
      return item.resourceKey === 'system' || item.code === 'menu.system' || name === 'menu.system' || name === '系统';
    });
    return systemGroup?.id || list?.[0]?.id || '';
  }, [list]);
  const [groupId, setGroupId] = useState<string>('');
  const activeGroupId = groupId || defaultGroupId;
  const hasActiveGroup = list.some((item) => item.id === activeGroupId);
  const activeGroup = list.find((item) => item.id === activeGroupId) || list[0];

  useEffect(() => {
    if (!list.length) return;
    if (!hasActiveGroup) {
      setGroupId(defaultGroupId);
    }
  }, [defaultGroupId, hasActiveGroup, list.length]);

  const handleClickItem = (item: ResourceProps) => {
    handleNavigate(item);
  };

  return (
    <Flex vertical gap={24} className={styles['functions']}>
      <Flex justify="space-between" align="center" gap={16} className={styles['functions-header']}>
        <ComEllipsis className={styles['title']}>{formatMessage('uns.functions')}</ComEllipsis>
        <Segmented<string>
          className={styles['functions-segmented']}
          value={activeGroupId}
          options={list.map((item) => ({
            label: translateResourceText(item.showName || item.code),
            value: item.id,
            title: translateResourceText(item.showName || item.code),
          }))}
          onChange={(value) => {
            setGroupId(value);
          }}
        />
      </Flex>
      <div className={styles['functions-grid']}>
        {activeGroup?.children?.map?.((c: any) => {
          // 新手导航 id
          let unsMenuId;
          if (c?.url === '/uns') {
            unsMenuId = 'home_route_uns';
          }
          return (
            <div id={unsMenuId} key={c.id} className={styles['functions-item']}>
              <ProCard
                classNames={{
                  card: styles['function-card'],
                  header: styles['function-card-header'],
                  headerTitle: styles['function-card-title'],
                }}
                header={{
                  title: translateResourceText(c.showName || c.code),
                  customIcon: c.iconComp ? (
                    <div className={styles['function-card-icon']}>{c.iconComp}</div>
                  ) : (
                    <MenuLucideIcon item={c} size={28} />
                  ),
                }}
                onClick={() => handleClickItem(c)}
                description={{
                  content: c.showDescription ? translateResourceText(c.showDescription) : undefined,
                  rows: 2,
                }}
              />
            </div>
          );
        })}
      </div>
    </Flex>
  );
};

export default Functions;
