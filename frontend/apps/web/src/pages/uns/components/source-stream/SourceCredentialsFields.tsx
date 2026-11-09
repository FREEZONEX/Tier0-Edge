import { type FC } from 'react';
import { Alert, Button, Collapse, Input, Select, Space, Tag } from 'antd';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import styles from './index.module.scss';
import type { SourceCredentialsDraft } from './source-credentials';

export { EMPTY_SOURCE_CREDENTIALS, type SourceCredentialsDraft } from './source-credentials';

const SourceCredentialsFields: FC<{
  value: SourceCredentialsDraft;
  onChange: (value: SourceCredentialsDraft) => void;
  canManage: boolean;
  retry?: boolean;
  existing?: boolean;
  disabled?: boolean;
  allowBearer?: boolean;
  allowAnonymous?: boolean;
  allowNamedSecrets?: boolean;
  allowTLS?: boolean;
  existingKeys?: string[];
  serverBearer?: boolean;
  fixedKind?: boolean;
}> = ({
  value,
  onChange,
  canManage,
  retry = false,
  existing = false,
  disabled = false,
  allowBearer = false,
  allowAnonymous = true,
  allowNamedSecrets = false,
  allowTLS = false,
  existingKeys = [],
  serverBearer = false,
  fixedKind = false,
}) => {
  const t = useTranslate();
  return (
    <div className={styles['full-width']}>
      <div className={styles.editor}>
        {value.kind === 'oauth2' ? (
          <label className={styles.field}>
            <span>{t('SourceStream.oauth.clientSecret')}</span>
            <Input.Password
              autoComplete="new-password"
              value={value.secret}
              disabled={disabled || !canManage}
              placeholder={existing ? t('SourceStream.keepCredentials') : undefined}
              onChange={(e) => onChange({ ...value, secret: e.target.value })}
            />
          </label>
        ) : (
          <>
            <label className={`${styles.field} ${styles['full-width']}`}>
              <Space>
                {t('SourceStream.authentication')}
                <HelpTooltip
                  title={t(canManage ? 'SourceStream.credentialHint' : 'SourceStream.credentialPermission')}
                />
              </Space>
              <Select
                aria-label={t('SourceStream.authentication')}
                value={value.kind}
                disabled={disabled || !canManage || retry || fixedKind}
                options={[
                  { value: 'none', label: t('SourceStream.authNone'), disabled: existing || !allowAnonymous },
                  ...(!serverBearer ? [{ value: 'basic', label: t('SourceStream.authBasic') }] : []),
                  ...(allowBearer ? [{ value: 'bearer', label: t('SourceStream.authBearer') }] : []),
                  ...((allowNamedSecrets || allowTLS) && !serverBearer
                    ? [
                        {
                          value: 'custom',
                          label: t(allowNamedSecrets ? 'SourceStream.authCustom' : 'SourceStream.tls.certificates'),
                        },
                      ]
                    : []),
                ]}
                onChange={(kind) => onChange({ ...value, kind, username: '', secret: '' })}
              />
            </label>
            {value.kind === 'bearer' && (
              <label className={styles.field}>
                <span>{t('SourceStream.authBearer')}</span>
                <Input.Password
                  autoComplete="new-password"
                  value={value.secret}
                  disabled={disabled || !canManage}
                  placeholder={existing ? t('SourceStream.keepCredentials') : undefined}
                  onChange={(e) => onChange({ ...value, secret: e.target.value })}
                />
              </label>
            )}
            {value.kind === 'custom' && <HelpTooltip title={t('SourceStream.authCustomHint')} />}
            {value.kind === 'basic' && (
              <>
                <label className={styles.field}>
                  <span>{t('SourceStream.username')}</span>
                  <Input
                    autoComplete="off"
                    placeholder={existing ? t('SourceStream.keepCredentials') : undefined}
                    value={value.username}
                    disabled={disabled || !canManage}
                    onChange={(event) => onChange({ ...value, username: event.target.value })}
                  />
                </label>
                <label className={styles.field}>
                  <span>{t('SourceStream.secret')}</span>
                  <Input.Password
                    autoComplete="new-password"
                    placeholder={existing ? t('SourceStream.keepCredentials') : undefined}
                    value={value.secret}
                    disabled={disabled || !canManage}
                    onChange={(event) => onChange({ ...value, secret: event.target.value })}
                  />
                </label>
              </>
            )}
          </>
        )}
      </div>
      {allowNamedSecrets && (
        <Collapse
          key={value.kind === 'custom' ? 'custom-secrets' : 'other-secrets'}
          defaultActiveKey={value.kind === 'custom' ? ['secrets'] : []}
          ghost
          items={[
            {
              key: 'secrets',
              label: (
                <Space>
                  {t('SourceStream.namedSecrets')}
                  <HelpTooltip title={t('SourceStream.namedSecretsHint')} />
                </Space>
              ),
              children: (
                <div className={styles.section}>
                  {existingKeys.length > 0 && (
                    <Space wrap>
                      <span>{t('SourceStream.savedSecretNames')}</span>
                      {existingKeys.map((name) => (
                        <Tag key={name}>{name}</Tag>
                      ))}
                    </Space>
                  )}
                  {(existingKeys.length > 0 || Boolean(value.namedSecrets?.length)) && existing && (
                    <Alert type="warning" showIcon message={t('SourceStream.replaceSecretsHint')} />
                  )}
                  {(value.namedSecrets ?? []).map((item, index) => (
                    <div key={index} className={styles.editor}>
                      <Input
                        aria-label={t('SourceStream.secretName')}
                        placeholder={t('SourceStream.secretName')}
                        value={item.name}
                        disabled={disabled || !canManage}
                        onChange={(e) =>
                          onChange({
                            ...value,
                            namedSecrets: value.namedSecrets?.map((row, i) =>
                              i === index ? { ...row, name: e.target.value } : row
                            ),
                          })
                        }
                      />
                      <Input.Password
                        aria-label={t('SourceStream.secret')}
                        autoComplete="new-password"
                        value={item.value}
                        disabled={disabled || !canManage}
                        onChange={(e) =>
                          onChange({
                            ...value,
                            namedSecrets: value.namedSecrets?.map((row, i) =>
                              i === index ? { ...row, value: e.target.value } : row
                            ),
                          })
                        }
                      />
                      <Button
                        disabled={disabled || !canManage}
                        onClick={() =>
                          onChange({ ...value, namedSecrets: value.namedSecrets?.filter((_, i) => i !== index) })
                        }
                      >
                        {t('common.delete')}
                      </Button>
                    </div>
                  ))}
                  <Button
                    disabled={disabled || !canManage}
                    onClick={() =>
                      onChange({ ...value, namedSecrets: [...(value.namedSecrets ?? []), { name: '', value: '' }] })
                    }
                  >
                    {t('SourceStream.addNamedSecret')}
                  </Button>
                </div>
              ),
            },
          ]}
        />
      )}
    </div>
  );
};

export default SourceCredentialsFields;
