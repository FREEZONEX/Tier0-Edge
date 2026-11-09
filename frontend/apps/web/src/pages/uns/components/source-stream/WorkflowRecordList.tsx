import ProTable from '@/components/pro-table';
import { useTranslate } from '@/hooks';
import { recordFields } from './workflow-records';
import { displayValue } from './workflow-data';

export default function WorkflowRecordList({ records }: { records: Record<string, unknown>[] }) {
  const t = useTranslate();
  return (
    <div>
      <p>{t('Ingest.recordCount', { count: records.length })}</p>
      <ProTable
        rowKey="__previewIndex"
        resizeable={false}
        scroll={{ x: 'max-content' }}
        pagination={{ pageSize: 5, showSizeChanger: false }}
        dataSource={records.map((record, index) => ({ record, __previewIndex: index }))}
        columns={recordFields(records).map((key) => ({
          title: key,
          key,
          render: (_, row) => displayValue(row.record[key]),
        }))}
      />
    </div>
  );
}
