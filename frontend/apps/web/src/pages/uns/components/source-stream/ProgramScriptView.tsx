import { useMemo, useState, type FC } from 'react';
import { Alert, Segmented, Space } from 'antd';
import { javascript } from '@codemirror/lang-javascript';
import { EditorView } from '@codemirror/view';
import ProCodemirror from '@/components/pro-codemirror';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import { useThemeStore } from '@/stores/theme-store';
import { formatProgramScript } from './program-format';
import { getStreamCodemirrorTheme } from './stream-codemirror-theme';
import styles from './index.module.scss';

const ProgramScriptView: FC<{ code: string }> = ({ code }) => {
  const t = useTranslate();
  const theme = useThemeStore((state) => state.theme);
  const [mode, setMode] = useState('formatted');
  const formatted = useMemo(() => formatProgramScript(code), [code]);
  return (
    <div className={styles.section}>
      <Space wrap>
        <Segmented
          value={mode}
          onChange={(next) => setMode(String(next))}
          options={[
            { value: 'formatted', label: t('SourceStream.formattedView') },
            { value: 'original', label: t('SourceStream.originalCode') },
          ]}
        />
        <HelpTooltip title={t('SourceStream.formatPreviewHint')} />
      </Space>
      {!formatted.valid && <Alert type="warning" showIcon message={t('SourceStream.formatFailed')} />}
      <ProCodemirror
        theme={getStreamCodemirrorTheme(theme)}
        value={mode === 'formatted' ? formatted.code : code}
        extensions={[javascript(), EditorView.lineWrapping]}
        readOnly
        editable={false}
        maxHeight="560px"
        showHint={false}
      />
    </div>
  );
};
export default ProgramScriptView;
