import type { FieldBinding, MetaDocument, MetaSchema, UnsMetadata, UnsRecord } from '@/apis/core-api/uns-metadata';
import { MAX_LENGTHS } from '@/utils/limits';

export const TOPIC_FIELD_LIMIT = 128;

export const topicNameTooLong = (name: string) => Array.from(name.trim()).length > MAX_LENGTHS.name;

export function topicDocument({
  mode,
  name,
  parentId,
  collectionId,
  fields,
  records,
}: {
  mode: 'records' | 'fields';
  name: string;
  parentId?: string;
  collectionId?: string;
  fields: FieldBinding[];
  records: Record<string, UnsRecord>;
}): MetaDocument {
  const base = { name: name.trim(), parentId: parentId || undefined, viewMode: mode };
  if (mode === 'records') return { ...base, collectionId };
  return { ...base, fields: fields.map((field) => ({ ...field, recordId: records[field.collectionId].id })) };
}

export const bindingKey = (field: Pick<FieldBinding, 'collectionId' | 'kind' | 'fieldPath'>) =>
  JSON.stringify([field.collectionId, field.kind, field.fieldPath]);

export function bindingSchema(field: FieldBinding, items: UnsMetadata[]): MetaSchema | undefined {
  const collection = items.find((item) => item.id === field.collectionId);
  let schema = field.kind === 'sample' ? collection?.data.sampleSchema : collection?.data.recordSchema;
  for (const part of field.fieldPath.split('.')) schema = schema?.properties?.[part];
  return schema;
}

export function fieldsValid(fields: FieldBinding[]) {
  return (
    fields.length > 0 &&
    fields.length <= TOPIC_FIELD_LIMIT &&
    fields.every(
      (field) =>
        field.name.trim() === field.name &&
        field.name.length > 0 &&
        /^[A-Za-z0-9][A-Za-z0-9_.:-]*$/.test(field.name) &&
        Array.from(field.name).length <= MAX_LENGTHS.name &&
        fields.filter((other) => other.name === field.name).length === 1
    )
  );
}

export function topicSchema(fields: FieldBinding[], items: UnsMetadata[]) {
  return {
    type: 'object',
    properties: Object.fromEntries(fields.map((field) => [field.name, bindingSchema(field, items) || {}])),
  };
}
