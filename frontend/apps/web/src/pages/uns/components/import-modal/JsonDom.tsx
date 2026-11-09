import CodeMirror from '@uiw/react-codemirror';
import { codemirrorTheme } from '@/theme/codemirror-theme.tsx';
import { json, jsonParseLinter } from '@codemirror/lang-json';
import { linter, lintGutter } from '@codemirror/lint';
import { placeholder } from '@/pages/uns/components/import-modal/data.ts';
import { useRef, useState } from 'react';
import { useSize } from 'ahooks';
import styles from '@/theme/codemirror.module.scss';
import { Copy } from '@/components/lucide-icon/carbon';
import { useClipboard, useTranslate } from '@/hooks';
import { App, Divider, Flex } from 'antd';
import ComButton from '../../../../components/com-button';
type Props = {
  onCloseModal: () => void;
  submitImport: (file: File) => Promise<boolean>;
  busy: boolean;
  submitting: boolean;
};

const JsonDom = ({ onCloseModal, submitImport, busy, submitting }: Props) => {
  const [jsonValue, setJsonValue] = useState<any>();
  const ref = useRef<HTMLDivElement>(null);
  const size = useSize(ref);
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const { copy } = useClipboard();
  const onSave = async () => {
    if (!jsonValue) {
      message.warning(formatMessage('uns.pleaseJSON'));
      return;
    }
    try {
      JSON.parse(jsonValue);
    } catch {
      message.error(formatMessage('uns.errorInTheSyntaxOfTheJSON'));
      return;
    }
    if (await submitImport(new File([jsonValue], 'uns.json', { type: 'application/json' }))) onCloseModal();
  };

  return (
    <Flex vertical style={{ height: '100%', overflow: 'hidden' }}>
      <div className="import-json-layout">
        <div ref={ref} className={`${styles['custom-theme']} import-json-editor`}>
          <div className={`import-json-quick-tip${jsonValue ? ' is-copy' : ''}`}>
            {jsonValue ? (
              <Copy
                style={{
                  cursor: 'pointer',
                  marginTop: 4,
                }}
                onClick={() => {
                  copy(jsonValue || JSON.stringify(JSON.parse(placeholder), null, 2));
                }}
              />
            ) : (
              <span>{formatMessage('uns.ctrlPQuickApplyExample')}</span>
            )}
          </div>
          <CodeMirror
            theme={codemirrorTheme}
            placeholder={placeholder}
            onChange={setJsonValue}
            editable={!busy}
            value={jsonValue}
            height={(size?.height || 32) - 32 + 'px'}
            extensions={[json(), linter(jsonParseLinter()), lintGutter()]}
            onKeyDownCapture={(e) => {
              if (e.ctrlKey && e.key === 'Enter') {
                e.preventDefault();
                e.stopPropagation();
                setJsonValue(placeholder);
              }
            }}
            onKeyDown={(e) => {
              if (e.ctrlKey && e.key === 'Enter') {
                e.preventDefault();
              }
            }}
          />
        </div>
      </div>
      <div style={{ flexShrink: 0 }}>
        <Divider style={{ margin: '16px 0' }} />
        <Flex align="center" gap={8} justify="flex-end">
          <ComButton onClick={onCloseModal}>{formatMessage('common.cancel')}</ComButton>
          <ComButton type="primary" onClick={onSave} loading={submitting} disabled={busy}>
            {formatMessage('common.import')}
          </ComButton>
        </Flex>
      </div>
    </Flex>
  );
};

export default JsonDom;
