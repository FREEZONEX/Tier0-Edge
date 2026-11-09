import { useRef, useState } from 'react';
import { Alert, Button, Drawer, Select, Space, Typography } from 'antd';
import { v4 as uuid } from 'uuid';
import { sourceCredentialApi, type SourceCredentialCatalog } from '@/apis/core-api/uns-v2';
import { hasPermission } from '@/utils/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import SourceCredentialsFields, { type SourceCredentialsDraft } from './SourceCredentialsFields';
import { namedSecretValues } from './source-credentials';

export default function SharedSourceCredentials({ sourceId }: { sourceId: string }) {
  const t = useTranslate();
  const [open, setOpen] = useState(false);
  const [catalog, setCatalog] = useState<SourceCredentialCatalog>();
  const [reference, setReference] = useState('');
  const [draft, setDraft] = useState<SourceCredentialsDraft>({ kind: 'basic', username: '', secret: '' });
  const [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [saved, setSaved] = useState('');
  const intent = useRef<{ signature: string; key: string }>();
  const pending = useRef(false);
  const canManage =
    hasPermission(ButtonPermission['uns.sourceCredentials']) && hasPermission(ButtonPermission['uns.sourceManage']);
  const act = async (work: () => Promise<void>) => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError('');
    try {
      await work();
    } catch (e) {
      setError((e as { msg?: string }).msg || t('fleet.credentials.failed'));
    } finally {
      pending.current = false;
      setBusy(false);
    }
  };
  const selected = catalog?.items.find((item) => item.credentialRef === reference);
  const values = namedSecretValues(draft);
  const valid =
    !!values &&
    (draft.kind === 'basic'
      ? !!draft.username && !!draft.secret
      : draft.kind === 'bearer'
        ? !!draft.secret
        : Object.keys(values).length > 0);
  return (
    <>
      <Button
        disabled={!canManage}
        onClick={() => {
          setOpen(true);
          setSaved('');
          setReference('');
          void act(async () => setCatalog(await sourceCredentialApi.list(sourceId)));
        }}
      >
        {t('fleet.credentials.manage')}
      </Button>
      <Drawer
        title={t('fleet.credentials.manage')}
        width={620}
        open={open}
        onClose={() => {
          setOpen(false);
          setDraft({ kind: 'basic', username: '', secret: '' });
          intent.current = undefined;
        }}
      >
        <Space direction="vertical" style={{ width: '100%' }}>
          <Typography.Text copyable>{sourceId}</Typography.Text>
          <Alert type="info" message={t('fleet.credentials.localBoundary')} />
          {error && <Alert type="error" message={error} />}
          {saved && <Alert type="success" message={<Typography.Text copyable>{saved}</Typography.Text>} />}
          <Select
            style={{ width: '100%' }}
            aria-label={t('fleet.credentials.reference')}
            value={reference}
            disabled={busy}
            options={[
              { value: '', label: t('fleet.credentials.newReference') },
              ...(catalog?.items || []).map((item) => ({
                value: item.credentialRef,
                label: `${item.credentialRef} · ${item.kind} · r${item.revision}`,
              })),
            ]}
            onChange={setReference}
          />
          <SourceCredentialsFields
            value={draft}
            onChange={setDraft}
            canManage={canManage}
            disabled={busy || !catalog}
            allowBearer={catalog?.protocol === 'http-api'}
            allowAnonymous={false}
            allowNamedSecrets={catalog?.protocol === 'http-api'}
          />
          <Button
            type="primary"
            loading={busy}
            disabled={!canManage || !catalog || !valid}
            onClick={() =>
              void act(async () => {
                const payload = {
                  id: sourceId,
                  sourceRevision: catalog!.sourceRevision,
                  ...(reference ? { credentialRef: reference } : {}),
                  expectedCredentialRevision: selected?.revision || 0,
                  kind: draft.kind,
                  username: draft.username,
                  secret: draft.secret,
                  ...(Object.keys(values || {}).length ? { values } : {}),
                };
                const signature = JSON.stringify(payload);
                if (intent.current?.signature !== signature) intent.current = { signature, key: uuid() };
                const result = await sourceCredentialApi.save({ ...payload, idempotencyKey: intent.current.key });
                setSaved(result.reference.credentialRef);
                setReference(result.reference.credentialRef);
                setDraft({ kind: 'basic', username: '', secret: '' });
                intent.current = undefined;
                setCatalog(await sourceCredentialApi.list(sourceId));
              })
            }
          >
            {t('common.save')}
          </Button>
          <Button
            disabled={busy}
            onClick={() => void act(async () => setCatalog(await sourceCredentialApi.list(sourceId)))}
          >
            {t('common.refresh')}
          </Button>
        </Space>
      </Drawer>
    </>
  );
}
