type QueryChanges = Record<string, string | undefined>;
type NavigationState = Record<string, unknown> & { unsOverviewParents: string[] };
const parentLimit = 32;

function stateObject(state: unknown): Record<string, unknown> {
  return state && typeof state === 'object' && !Array.isArray(state) ? (state as Record<string, unknown>) : {};
}
export function preserveOverviewState(state: unknown): NavigationState {
  const object = stateObject(state);
  const parents = Array.isArray(object.unsOverviewParents)
    ? object.unsOverviewParents
        .filter((value): value is string => typeof value === 'string' && value.length <= 16384)
        .slice(-parentLimit)
    : [];
  return { ...object, unsOverviewParents: parents };
}

export function changeOverviewSearch(search: string, changes: QueryChanges): string {
  const params = new URLSearchParams(search);
  Object.entries(changes).forEach(([key, value]) =>
    value === undefined ? params.delete(key) : params.set(key, value)
  );
  return params.toString();
}

export function relationshipGraphLocation(search: string) {
  return { pathname: '/uns/topology', search: changeOverviewSearch(search, { view: 'graph' }) };
}

export function enterOverviewObject(search: string, changes: QueryChanges, state: unknown) {
  const navigation = preserveOverviewState(state);
  return {
    search: changeOverviewSearch(search, changes),
    state: { ...navigation, unsOverviewParents: [...navigation.unsOverviewParents, search].slice(-parentLimit) },
  };
}

// 返回目标只有本站当前路由的查询参数，不使用 history(-1)，避免深链接跳出站点。
export function overviewParent(search: string, state: unknown, target: 'previous' | 'overview' | 'focus' = 'previous') {
  const navigation = preserveOverviewState(state);
  const current = new URLSearchParams(search);
  for (let index = navigation.unsOverviewParents.length - 1; index >= 0; index -= 1) {
    const parent = new URLSearchParams(navigation.unsOverviewParents[index]);
    const record = !!parent.get('recordId') && !!parent.get('collectionId');
    const match =
      target === 'previous' ||
      (target === 'overview'
        ? !parent.get('id') && !record
        : !record && parent.get('kind') === current.get('kind') && parent.get('id') === current.get('id'));
    if (match)
      return {
        search: parent.toString(),
        state: { ...navigation, unsOverviewParents: navigation.unsOverviewParents.slice(0, index) },
      };
  }
  const changes: QueryChanges = {
    recordId: undefined,
    samplePage: undefined,
    time: undefined,
    from: undefined,
    to: undefined,
  };
  if (target === 'overview' || (!current.get('recordId') && target === 'previous'))
    Object.assign(changes, {
      kind: undefined,
      id: undefined,
      collectionId: undefined,
      recordPage: undefined,
      page: undefined,
    });
  return { search: changeOverviewSearch(search, changes), state: { ...navigation, unsOverviewParents: [] } };
}
