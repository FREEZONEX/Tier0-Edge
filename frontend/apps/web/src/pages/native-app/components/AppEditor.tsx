import { useRef, useState } from 'react';
import { Alert, Button, Form, Input } from 'antd';
import { v4 as uuid } from 'uuid';
import { MAX_LENGTHS } from '@/utils/limits';
import styles from '@/pages/project/components/EditAppModal.module.scss';
import BusinessRolesPanel from '@/pages/account-management/components/BusinessRolesPanel';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import { saveAppDetails, type NativeAppInstallation } from '@/apis/core-api/native-app';

export type AppEditorState = { mode: 'create' } | { mode: 'edit' | 'access'; app: NativeAppInstallation };

function DetailsEditor({
  editor,
  onClose,
  onSaved,
}: {
  editor: AppEditorState;
  onClose: () => void;
  onSaved: () => void;
}) {
  const t = useTranslate();
  const app = editor.mode === 'create' ? undefined : editor.app;
  const [id] = useState(() => app?.appId || uuid());
  const [name, setName] = useState(app?.name || '');
  const [description, setDescription] = useState(app?.description || '');
  const attempt = useRef({ payload: '', key: '' });
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const canSave = !!name.trim();
  const submit = async () => {
    if (!canSave || saving) return;
    setSaving(true);
    setError('');
    try {
      const payload = JSON.stringify([id, name.trim(), description.trim()]);
      if (attempt.current.payload !== payload) attempt.current = { payload, key: uuid() };
      await saveAppDetails(id, name.trim(), description.trim(), attempt.current.key, app);
      onSaved();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('nativeApp.actionFailed'));
    } finally {
      setSaving(false);
    }
  };
  return (
    <ProModal
      open
      width={544}
      styles={{ body: { maxHeight: '72vh', overflowY: 'auto' } }}
      fullScreenable={false}
      title={t(app ? 'common.edit' : 'nativeApp.create')}
      onCancel={() => {
        if (!saving) onClose();
      }}
    >
      {error && <Alert type="error" message={error} />}
      <Form layout="vertical" requiredMark={false} onFinish={() => void submit()}>
        <>
          <Form.Item label={t('common.name')} htmlFor="native-app-name" required style={{ marginBottom: 12 }}>
            <Input
              id="native-app-name"
              autoFocus
              placeholder={t('apps.namePlaceholder')}
              maxLength={MAX_LENGTHS.name}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Form.Item>
          {app && (
            <div className={styles['meta-grid']} style={{ marginBottom: 12 }}>
              <div>
                <label htmlFor="native-app-id" className={styles['meta-label']}>
                  {t('common.appId')}
                </label>
                <Input id="native-app-id" value={app.appId} disabled />
              </div>
              <div>
                <label htmlFor="native-app-version" className={styles['meta-label']}>
                  {t('nativeApp.version')}
                </label>
                <Input id="native-app-version" value={app.version || '—'} disabled />
              </div>
            </div>
          )}
          <Form.Item
            label={t('common.description')}
            htmlFor="native-app-description"
            className={styles.descriptionItem}
          >
            <Input.TextArea
              id="native-app-description"
              rows={3}
              showCount
              placeholder={t('apps.descriptionPlaceholder')}
              maxLength={MAX_LENGTHS.description}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </Form.Item>
        </>
        <div className={styles.actions}>
          <Button color="default" variant="filled" disabled={saving} onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button type="primary" htmlType="submit" loading={saving} disabled={!canSave}>
            {t('common.save')}
          </Button>
        </div>
      </Form>
    </ProModal>
  );
}

export default function AppEditor(props: Parameters<typeof DetailsEditor>[0]) {
  const t = useTranslate();
  if (props.editor.mode === 'access')
    return (
      <ProModal
        open
        width={900}
        fullScreenable={false}
        title={t('nativeApp.access') + ' · ' + props.editor.app.name}
        onCancel={props.onClose}
      >
        <BusinessRolesPanel appId={props.editor.app.appId} />
      </ProModal>
    );
  return <DetailsEditor {...props} />;
}
