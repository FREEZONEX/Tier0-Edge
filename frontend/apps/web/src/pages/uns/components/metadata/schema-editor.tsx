import HelpTooltip from '@/components/help-tooltip';
import { Alert, Checkbox, Input, Select, Space, Tabs } from 'antd';
import { Add, TrashCan } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import ProTable from '@/components/pro-table';
import { useTranslate } from '@/hooks';
import type { MetaSchema } from '@/apis/core-api/uns-metadata';
import styles from './index.module.scss';

export const emptySchema: MetaSchema = { type: 'object', properties: {}, required: [], additionalProperties: false };
export const json = (value: unknown) => JSON.stringify(value, null, 2);
export const fieldPaths = (schema?: MetaSchema, prefix = ''): string[] =>
  Object.entries(schema?.properties || {}).flatMap(([name, field]) => {
    const path = prefix ? `${prefix}.${name}` : name;
    return [path, ...(field.type === 'object' ? fieldPaths(field, path) : [])];
  });

export default function SchemaEditor({ value = '', onChange }: { value?: string; onChange?: (value: string) => void }) {
  const t = useTranslate();
  let schema: MetaSchema | undefined;
  try {
    schema = JSON.parse(value || json(emptySchema));
  } catch {
    /* Keep invalid JSON editable. */
  }
  const fields = Object.entries(schema?.properties || {});
  const update = (next: MetaSchema) => onChange?.(json(next));
  const edit = (index: number, name: string, field: MetaSchema, required: boolean) => {
    if (!schema || fields.some(([key], i) => i !== index && key === name)) return;
    const previous = fields[index][0];
    const next = fields.map((entry, i) => (i === index ? [name, field] : entry));
    const requiredFields = [...(schema.required || []).filter((key) => key !== previous), ...(required ? [name] : [])];
    update({
      ...schema,
      properties: Object.fromEntries(next),
      required: requiredFields.length ? requiredFields : undefined,
    });
  };
  return (
    <Tabs
      items={[
        {
          key: 'fields',
          label: t('UnsMeta.fields'),
          children: (
            <>
              {!schema || schema.type !== 'object' ? (
                <Alert type="error" message={t('UnsMeta.invalidSchema')} />
              ) : (
                <>
                  <ProTable
                    rowKey="index"
                    pagination={false}
                    resizeable={false}
                    dataSource={fields.map(([name, field], index) => ({ name, field, index }))}
                    columns={[
                      {
                        title: t('UnsMeta.field'),
                        dataIndex: 'name',
                        render: (name: string, row) => (
                          <Input
                            aria-label={t('UnsMeta.field')}
                            value={name}
                            onChange={(e) =>
                              edit(row.index, e.target.value, row.field, Boolean(schema?.required?.includes(name)))
                            }
                          />
                        ),
                      },
                      {
                        title: t('UnsMeta.type'),
                        render: (_, row) => (
                          <Select
                            aria-label={t('UnsMeta.type')}
                            value={row.field.type}
                            className={styles.full}
                            options={['string', 'number', 'integer', 'boolean', 'object', 'array', 'null'].map(
                              (type) => ({ value: type, label: type })
                            )}
                            onChange={(type) =>
                              edit(
                                row.index,
                                row.name,
                                {
                                  type,
                                  ...(type === 'object' ? { properties: {}, additionalProperties: false } : {}),
                                  ...(type === 'array' ? { items: { type: 'string' } } : {}),
                                },
                                Boolean(schema?.required?.includes(row.name))
                              )
                            }
                          />
                        ),
                      },
                      {
                        title: t('UnsMeta.unit'),
                        render: (_, row) => (
                          <Input
                            aria-label={t('UnsMeta.unit')}
                            value={row.field.unit}
                            onChange={(e) =>
                              edit(
                                row.index,
                                row.name,
                                { ...row.field, unit: e.target.value },
                                Boolean(schema?.required?.includes(row.name))
                              )
                            }
                          />
                        ),
                      },
                      {
                        title: t('UnsMeta.required'),
                        width: 90,
                        render: (_, row) => (
                          <Checkbox
                            aria-label={t('UnsMeta.required')}
                            checked={schema?.required?.includes(row.name)}
                            onChange={(e) => edit(row.index, row.name, row.field, e.target.checked)}
                          />
                        ),
                      },
                      {
                        title: '',
                        width: 50,
                        render: (_, row) => (
                          <ComButton
                            type="text"
                            aria-label={t('common.delete')}
                            icon={<TrashCan />}
                            onClick={() =>
                              update({
                                ...schema!,
                                properties: Object.fromEntries(fields.filter((_, i) => i !== row.index)),
                                required: schema?.required?.filter((name) => name !== row.name),
                              })
                            }
                          />
                        ),
                      },
                    ]}
                  />
                  <Space className={styles['section-actions']}>
                    <ComButton
                      icon={<Add />}
                      onClick={() => {
                        let n = fields.length + 1;
                        while (schema?.properties?.[`field${n}`]) n++;
                        update({
                          ...schema!,
                          properties: { ...schema?.properties, [`field${n}`]: { type: 'string' } },
                        });
                      }}
                    >
                      {t('UnsMeta.addField')}
                    </ComButton>
                    <Checkbox
                      checked={schema.additionalProperties !== false}
                      onChange={(e) => update({ ...schema!, additionalProperties: e.target.checked })}
                    >
                      {t('UnsMeta.allowAdditional')}
                    </Checkbox>
                  </Space>
                  <HelpTooltip title={t('UnsMeta.schemaHint')} />
                </>
              )}
            </>
          ),
        },
        {
          key: 'json',
          label: t('UnsMeta.schemaJson'),
          children: (
            <Input.TextArea
              className={styles.code}
              aria-label={t('UnsMeta.schemaJson')}
              rows={14}
              value={value}
              onChange={(e) => onChange?.(e.target.value)}
            />
          ),
        },
      ]}
    />
  );
}
