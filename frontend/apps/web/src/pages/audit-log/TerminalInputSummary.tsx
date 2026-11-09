import { Alert, Typography } from 'antd';
import { useTranslate } from '@/hooks';
import styles from './index.module.scss';

function parseSummary(raw?: string) {
  try {
    const summary = JSON.parse(raw || '{}')?.inputSummary;
    if (summary?.source !== 'terminalInput' || !Array.isArray(summary.lines)) return;
    return {
      lines: summary.lines.filter((line: unknown): line is string => typeof line === 'string'),
      omittedLines: Number.isSafeInteger(summary.omittedLines) && summary.omittedLines >= 0 ? summary.omittedLines : 0,
      redactedLines:
        Number.isSafeInteger(summary.redactedLines) && summary.redactedLines >= 0 ? summary.redactedLines : 0,
      truncated: summary.truncated === true,
    };
  } catch {
    return;
  }
}

export default function TerminalInputSummary({ detailJson }: { detailJson?: string }) {
  const t = useTranslate();
  const summary = parseSummary(detailJson);
  if (!summary) return null;
  return (
    <section className={styles.detailSection}>
      <div className={styles.sectionHeader}>{t('auditLog.terminalInput')}</div>
      <Alert type="info" message={t('auditLog.terminalInputHint')} />
      <pre className={styles.detailJson}>
        {summary.lines.length ? summary.lines.join('\n') : t('auditLog.terminalInputEmpty')}
      </pre>
      <Typography.Text type="secondary">
        {t('auditLog.terminalInputCounts', { omitted: summary.omittedLines, redacted: summary.redactedLines })}
      </Typography.Text>
      {summary.truncated && <Alert type="warning" message={t('auditLog.terminalInputTrunc')} />}
    </section>
  );
}
