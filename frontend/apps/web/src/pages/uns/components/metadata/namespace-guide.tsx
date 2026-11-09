import { Network, Table2, Folder, FolderPlus, Plug, Plus, ChevronRight } from 'lucide-react';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import styles from './namespace-guide.module.scss';

export default function NamespaceGuide({
  onCreateData,
  onCreateFolder,
  onConnect,
}: {
  onCreateData?: () => void;
  onCreateFolder?: () => void;
  onConnect?: () => void;
}) {
  const t = useTranslate();
  return (
    <section className={styles.guide} aria-labelledby="namespace-guide-title">
      <div className={styles.inner}>
        <span className={styles.heroIcon}>
          <Network size={28} />
        </span>
        <div className={styles.heading}>
          <h2 id="namespace-guide-title">{t('NamespaceGuide.title')}</h2>
          <p>{t('NamespaceGuide.hint')}</p>
        </div>
        <ol className={styles.steps}>
          {['dataset', 'source', 'explore'].map((step, index) => (
            <li key={step}>
              <span>{index + 1}</span>
              {t(`NamespaceGuide.step.${step}`)}
            </li>
          ))}
        </ol>
        {(onCreateData || onCreateFolder) && (
          <>
            <h3>{t('NamespaceGuide.manual')}</h3>
            <div className={styles.cards}>
              {onCreateData && (
                <div className={styles.row}>
                  <span className={styles.icon}>
                    <Table2 size={22} />
                  </span>
                  <div className={styles.copy}>
                    <strong>{t('NamespaceGuide.step.dataset')}</strong>
                    <p>{t('NamespaceGuide.records')}</p>
                  </div>
                  <ComButton icon={<Plus size={17} />} onClick={onCreateData}>
                    {t('NamespaceGuide.newDataset')}
                  </ComButton>
                </div>
              )}
              {onCreateFolder && (
                <div className={styles.row}>
                  <span className={styles.icon}>
                    <Folder size={22} />
                  </span>
                  <div className={styles.copy}>
                    <strong>{t('NamespaceGuide.folders')}</strong>
                    <p>{t('NamespaceGuide.foldersHint')}</p>
                  </div>
                  <ComButton icon={<FolderPlus size={17} />} onClick={onCreateFolder}>
                    {t('UnsUi.createFolder')}
                  </ComButton>
                </div>
              )}
            </div>
          </>
        )}
        {onConnect && (
          <div className={styles.cards}>
            <div className={styles.row}>
              <span className={styles.icon}>
                <Plug size={22} />
              </span>
              <div className={styles.copy}>
                <strong>{t('UnsConnect.connect')}</strong>
                <p>{t('NamespaceGuide.connectHint')}</p>
              </div>
              <ComButton aria-label={t('UnsConnect.connect')} icon={<ChevronRight size={18} />} onClick={onConnect} />
            </div>
          </div>
        )}
      </div>
    </section>
  );
}
