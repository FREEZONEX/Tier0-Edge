import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Collapse, Space, Tag, Upload } from 'antd';
import { Upload as UploadIcon } from '@carbon/icons-react';
import type { SourceDocument } from '@/apis/core-api/source-stream';
import { useTranslate } from '@/hooks';
import { supportsTLSCredentials, type SourceCredentialsDraft } from './source-credentials';
import styles from './index.module.scss';

const fields = ['caCertificatePem', 'clientCertificatePem', 'clientPrivateKeyPem'] as const;

export default function SourceTLSFields({
  document,
  value,
  onChange,
  disabled,
  onReadingChange,
}: {
  document: SourceDocument;
  value: SourceCredentialsDraft;
  onChange: (value: SourceCredentialsDraft) => void;
  disabled: boolean;
  onReadingChange: (reading: boolean) => void;
}) {
  const t = useTranslate();
  const [error, setError] = useState(false);
  const [reading, setReading] = useState(false);
  const latest = useRef(value);
  const active = useRef(false);
  const target = `${document.protocol}:${document.role}:${document.endpoint}`;
  const currentTarget = useRef(target);
  useEffect(() => {
    latest.current = value;
    currentTarget.current = target;
  }, [value, target]);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
      onReadingChange(false);
    };
  }, [onReadingChange]);
  if (!supportsTLSCredentials(document)) return null;
  return (
    <Collapse
      ghost
      items={[
        {
          key: 'tls',
          label: t('SourceStream.tls.certificates'),
          children: (
            <div className={styles.section}>
              {error && <Alert type="error" showIcon message={t('SourceStream.tls.invalidFile')} />}
              {fields.map((field) => (
                <Space key={field} wrap>
                  <span>{t(`SourceStream.tls.${field}`)}</span>
                  <Upload
                    accept=".pem,.crt,.cer,.key"
                    disabled={disabled || reading}
                    showUploadList={false}
                    beforeUpload={async (file) => {
                      setReading(true);
                      onReadingChange(true);
                      setError(false);
                      try {
                        if (file.size > 65536) throw new Error('certificate size');
                        const pem = await file.text();
                        const block =
                          field === 'clientPrivateKeyPem'
                            ? /-----BEGIN (?:RSA |EC )?PRIVATE KEY-----/
                            : /-----BEGIN CERTIFICATE-----/;
                        if (!block.test(pem)) throw new Error('certificate encoding');
                        if (active.current && currentTarget.current === target)
                          onChange({ ...latest.current, tls: { ...latest.current.tls, [field]: pem } });
                      } catch {
                        if (active.current && currentTarget.current === target) setError(true);
                      } finally {
                        if (active.current) setReading(false);
                        if (active.current) onReadingChange(false);
                      }
                      return Upload.LIST_IGNORE;
                    }}
                  >
                    <Button
                      icon={<UploadIcon size={16} />}
                      disabled={disabled || reading}
                      aria-label={t(`SourceStream.tls.${field}`)}
                    >
                      {t('common.upload')}
                    </Button>
                  </Upload>
                  {value.tls?.[field] && (
                    <>
                      <Tag>{t('SourceStream.tls.selected')}</Tag>
                      <Button
                        type="text"
                        disabled={disabled || reading}
                        onClick={() => onChange({ ...value, tls: { ...value.tls, [field]: undefined } })}
                      >
                        {t('common.delete')}
                      </Button>
                    </>
                  )}
                </Space>
              ))}
            </div>
          ),
        },
      ]}
    />
  );
}
