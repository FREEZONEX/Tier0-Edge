import { useState, type ReactNode } from 'react';
import { Alert, App, Input, Modal } from 'antd';
import { v4 as uuid } from 'uuid';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import { hasPermission } from '@/utils/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import SharedSourceCredentials from '../source-stream/SharedSourceCredentials';
import { saveConfiguration, unsV2Api } from '@/apis/core-api/uns-v2';

export interface SharedConfiguration {
  kind: 'source' | 'stream' | 'app';
  id: string;
  revision: number;
  data: Record<string, unknown> & { name: string; schemaVersion?: number };
}
function writable(row: SharedConfiguration) {
  return Object.fromEntries(
    Object.entries(row.data).filter(
      ([key]) => !['releases', 'configRevision', 'desiredState', 'credentials'].includes(key)
    )
  );
}
export default function ConfigEditor({
  row,
  onSaved,
  children,
}: {
  row: SharedConfiguration;
  onSaved: () => void;
  children?: ReactNode;
}) {
  const t = useTranslate();
  const { message, modal } = App.useApp();
  const [open, setOpen] = useState(false);
  const [text, setText] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(false);
  const editable =
    row.data.schemaVersion === 1 &&
    hasPermission(
      row.kind === 'source'
        ? ButtonPermission['uns.sourceManage']
        : row.kind === 'stream'
          ? ButtonPermission['uns.streamManage']
          : ButtonPermission['uns.appManage']
    );
  const save = async () => {
    let data: unknown;
    try {
      data = JSON.parse(text);
      if (!data || typeof data !== 'object' || Array.isArray(data)) throw new Error('Invalid configuration');
    } catch {
      setError(true);
      return;
    }
    setSaving(true);
    try {
      await saveConfiguration(row.kind, row.id, row.revision, writable(row), data as Record<string, unknown>);
      message.success(t('UnsMeta.saved'));
      setOpen(false);
      onSaved();
    } finally {
      setSaving(false);
    }
  };
  return (
    <>
      <ComButton
        type="link"
        onClick={() => {
          setText(JSON.stringify(writable(row), null, 2));
          setError(false);
          setOpen(true);
        }}
      >
        {children || row.data.name}
      </ComButton>
      <Modal
        open={open}
        width={880}
        title={`${row.kind} · ${row.data.name}`}
        onCancel={() => setOpen(false)}
        onOk={save}
        confirmLoading={saving}
        okButtonProps={{ disabled: !editable }}
        okText={t('common.save')}
        cancelText={t('common.cancel')}
        maskClosable={!saving}
        footer={(_, { OkBtn, CancelBtn }) => (
          <>
            {row.kind === 'source' && editable && hasPermission(ButtonPermission['uns.metadataDelete']) && (
              <ComButton
                danger
                disabled={saving}
                onClick={() => {
                  const idempotencyKey = uuid();
                  modal.confirm({
                    title: t('fleet.lifecycle.archiveSource'),
                    content: t('fleet.lifecycle.archiveSourceHelp'),
                    okButtonProps: { danger: true },
                    onOk: async () => {
                      await unsV2Api.resourceWrite('/sources/archive', {
                        id: row.id,
                        expectedRevision: row.revision,
                        idempotencyKey,
                      });
                      setOpen(false);
                      onSaved();
                    },
                  });
                }}
              >
                {t('fleet.lifecycle.archiveSource')}
              </ComButton>
            )}
            <CancelBtn />
            <OkBtn />
          </>
        )}
      >
        <Alert type="info" message={t('Collections.configOnly')} />
        {row.kind === 'source' && row.data.schemaVersion === 1 && <SharedSourceCredentials sourceId={row.id} />}
        {error && <Alert type="error" message={t('UnsMeta.invalidSchema')} />}
        <Input.TextArea
          aria-label={t('Collections.configuration')}
          rows={20}
          value={text}
          onChange={(event) => setText(event.target.value)}
          readOnly={!editable}
        />
      </Modal>
    </>
  );
}
