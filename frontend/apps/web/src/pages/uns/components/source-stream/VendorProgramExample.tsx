import { useState, type FC } from 'react';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import type { Source } from '@/apis/core-api/source-stream';
import InternalResourceSelect from './InternalResourceSelect';
import { registrationSnippet } from './program-document';
import { vendorCollectionBody } from './vendor-program';
import styles from './index.module.scss';

const VendorProgramExample: FC<{
  source: Source;
  resourceRef: string;
  onInsert: (snippet: string) => void;
}> = ({ source, resourceRef, onInsert }) => {
  const t = useTranslate();
  const [topic, setTopic] = useState<string>();
  const resource = source.resources.find((item) => item.id === resourceRef);
  if (!source.document.sourceType || resource?.metadata?.readOnly !== true) return null;
  return (
    <div className={styles.section}>
      <label className={styles.field}>
        <span>{t('SourceStream.output')}</span>
        <InternalResourceSelect write value={topic ? [topic] : []} onChange={(ids) => setTopic(ids.at(-1))} />
      </label>
      <ComButton
        disabled={!topic}
        onClick={() => {
          if (!topic) return;
          const body = vendorCollectionBody(source, resourceRef, topic, `collect-${source.id}-${Date.now()}`);
          if (body) onInsert(registrationSnippet(body));
        }}
      >
        {t('SourceStream.vendorExample')}
      </ComButton>
      <HelpTooltip title={t('SourceStream.vendorExampleHint')} />
    </div>
  );
};

export default VendorProgramExample;
