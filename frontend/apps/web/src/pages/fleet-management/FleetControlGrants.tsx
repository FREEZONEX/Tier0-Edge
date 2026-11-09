import { useRef, useState } from 'react';
import { Alert, App, Button, Drawer, Form, Input, InputNumber, Select, Space, Switch, Typography } from 'antd';
import { fleetAction } from '@/apis/core-api/fleet-acquisition';
import type { ControlCapability, ControlGrant } from '@/apis/core-api/fleet-control';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
import { useTranslate } from '@/hooks';

export default function FleetControlGrants() {
  const t = useTranslate();
  const { modal } = App.useApp();
  const [open, setOpen] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState('');
  const [centerId, setCenterId] = useState(''),
    [catalog, setCatalog] = useState<ControlCapability[]>([]);
  const [selected, setSelected] = useState<ControlCapability>(),
    [grant, setGrant] = useState<ControlGrant>();
  const [schema, setSchema] = useState('{}'),
    [ttl, setTtl] = useState(60),
    [interval, setInterval] = useState(5),
    [enabled, setEnabled] = useState(false),
    [allowAutomatic, setAllowAutomatic] = useState(false);
  const [clean, setClean] = useState('');
  const submitting = useRef(false);
  const signature = JSON.stringify({ schema, ttl, interval, enabled, allowAutomatic });
  const dirty = !!selected && signature !== clean;
  const allowed = ['Control.read', 'Control.write', 'Control.manage', 'FleetConnection.configure'].every((key) =>
    hasPermission(ButtonPermission[key as keyof typeof ButtonPermission])
  );
  const act = async (work: () => Promise<void>) => {
    if (submitting.current) return;
    submitting.current = true;
    setBusy(true);
    setError('');
    try {
      await work();
    } catch (e) {
      setError((e as { msg?: string }).msg || (e as Error).message || t('fleet.control.failed'));
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  };
  const load = async (capability: ControlCapability) => {
    let value: ControlGrant | undefined;
    try {
      value = await fleetAction<ControlGrant>(
        'control-grants/get',
        { centerId, controlCode: capability.code },
        { silent: true }
      );
    } catch (e) {
      if ((e as { code?: number }).code !== 404) throw e;
    }
    const fields = {
      schema: JSON.stringify(value?.inputSchema || capability.contract.inputSchema, null, 2),
      ttl: (value?.maxTtlMs || 60000) / 1000,
      interval: (value?.minIntervalMs || 5000) / 1000,
      enabled: value?.enabled || false,
      allowAutomatic: value?.allowAutomatic || false,
    };
    setSelected(capability);
    setGrant(value);
    setSchema(fields.schema);
    setTtl(fields.ttl);
    setInterval(fields.interval);
    setEnabled(fields.enabled);
    setAllowAutomatic(fields.allowAutomatic);
    setClean(JSON.stringify(fields));
  };
  const choose = (work: () => void) => {
    if (dirty) modal.confirm({ title: t('fleet.workspace.unsaved'), content: t('fleet.control.discard'), onOk: work });
    else work();
  };
  const save = async () => {
    if (!selected) return;
    const inputSchema: unknown = JSON.parse(schema);
    if (!inputSchema || typeof inputSchema !== 'object' || Array.isArray(inputSchema))
      throw new Error(t('fleet.control.schemaInvalid'));
    const value = await fleetAction<ControlGrant>('control-grants/save', {
      centerId,
      controlCode: selected.code,
      capabilityVersion: selected.capabilityVersion,
      inputSchema,
      maxTtlMs: ttl * 1000,
      minIntervalMs: interval * 1000,
      enabled,
      allowAutomatic,
      expectedRevision: grant?.revision || 0,
    });
    setGrant(value);
    setClean(signature);
  };
  if (!allowed) return null;
  return (
    <>
      <Button
        onClick={() => {
          setOpen(true);
          void act(async () => {
            const catalog = await fleetAction<{ list: ControlCapability[]; centerId?: string }>(
              'control-grants/catalog',
              {}
            );
            setCenterId(catalog.centerId || '');
            setCatalog(catalog.list);
          });
        }}
      >
        {t('fleet.control.grants')}
      </Button>
      <Drawer
        title={t('fleet.control.grants')}
        width={720}
        open={open}
        onClose={() =>
          choose(() => {
            setOpen(false);
            setSelected(undefined);
          })
        }
      >
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Alert type="info" showIcon message={t('fleet.control.grantNotice')} />
          {error && <Alert type="error" showIcon message={error} />}
          {!centerId && <Alert type="warning" showIcon message={t('fleet.control.noCenter')} />}
          <Typography.Text>
            {t('fleet.control.center')}: {centerId || '-'}
          </Typography.Text>
          <Form layout="vertical" disabled={busy || !centerId}>
            <Form.Item label={t('fleet.control.capability')}>
              <Select
                aria-label={t('fleet.control.capability')}
                value={selected?.code}
                options={catalog.map((c) => ({ label: `${c.name} · ${c.code}`, value: c.code }))}
                onChange={(code) =>
                  choose(() => {
                    const found = catalog.find((c) => c.code === code);
                    if (found) void act(() => load(found));
                  })
                }
              />
            </Form.Item>
            {selected && (
              <>
                <Typography.Text copyable>{selected.capabilityVersion}</Typography.Text>
                {grant && grant.capabilityVersion !== selected.capabilityVersion && (
                  <Alert type="warning" showIcon message={t('fleet.control.versionChanged')} />
                )}
                <Form.Item label={t('fleet.control.restrictions')}>
                  <Input.TextArea
                    aria-label={t('fleet.control.restrictions')}
                    rows={12}
                    value={schema}
                    onChange={(e) => setSchema(e.target.value)}
                  />
                </Form.Item>
                <Space align="start" wrap>
                  <Form.Item label={t('fleet.control.maxTtl')}>
                    <InputNumber
                      aria-label={t('fleet.control.maxTtl')}
                      min={1}
                      max={300}
                      precision={0}
                      value={ttl}
                      onChange={(value) => setTtl(value || 1)}
                    />
                  </Form.Item>
                  <Form.Item label={t('fleet.control.minInterval')}>
                    <InputNumber
                      aria-label={t('fleet.control.minInterval')}
                      min={1}
                      max={86400}
                      precision={0}
                      value={interval}
                      onChange={(value) => setInterval(value || 1)}
                    />
                  </Form.Item>
                  <Form.Item label={t('fleet.control.authorized')}>
                    <Switch aria-label={t('fleet.control.authorized')} checked={enabled} onChange={setEnabled} />
                  </Form.Item>
                </Space>
                <Form.Item label={t('fleet.control.allowAutomatic')}>
                  <Switch
                    aria-label={t('fleet.control.allowAutomatic')}
                    checked={allowAutomatic}
                    disabled={!hasPermission(ButtonPermission['Control.automate'])}
                    onChange={setAllowAutomatic}
                  />
                </Form.Item>
                <Button type="primary" loading={busy} disabled={!dirty || !centerId} onClick={() => void act(save)}>
                  {t('fleet.control.saveGrant')}
                </Button>
              </>
            )}
          </Form>
        </Space>
      </Drawer>
    </>
  );
}
