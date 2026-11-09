// OpenAPI 3.0 describes the wire shape; the embedded shared JSON Schema remains
// authoritative for array cardinality and SDK semantic validation.
export function discoveryOpenAPI30Schema(value) {
  if (Array.isArray(value)) return value.map(discoveryOpenAPI30Schema);
  if (!value || typeof value !== 'object') return value;
  const result = {};
  for (const [key, item] of Object.entries(value)) {
    if (['$schema', '$id', '$defs', 'if', 'then', 'else'].includes(key)) continue;
    if (key === 'const') result.enum = [item];
    else if (['contains', 'minContains', 'maxContains'].includes(key)) result[`x-json-schema-${key}`] = item;
    else if (key === '$ref') result.$ref = item.replace('#/$defs/', '#/components/schemas/PlatformV2Discovery_');
    else result[key] = discoveryOpenAPI30Schema(item);
  }
  if (value.if) {
    const condition = discoveryOpenAPI30Schema(value.if);
    const branches = [...(result.allOf ?? [])];
    if (value.then) branches.push({anyOf: [{not: condition}, discoveryOpenAPI30Schema(value.then)]});
    if (value.else) branches.push({anyOf: [condition, discoveryOpenAPI30Schema(value.else)]});
    if (branches.length) result.allOf = branches;
  }
  return result;
}
