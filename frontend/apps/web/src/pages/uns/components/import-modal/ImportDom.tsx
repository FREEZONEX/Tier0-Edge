import cx from 'classnames';
import { Upload as UploadIcon } from '@/components/lucide-icon/carbon';
import { App, Divider, Flex, Upload, type UploadFile } from 'antd';
import { useState } from 'react';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
const { Dragger } = Upload;

type Props = {
  onCloseModal: () => void;
  submitImport: (file: File) => Promise<boolean>;
  busy: boolean;
  submitting: boolean;
  fillHeight?: boolean;
};

const ImportDom = ({ onCloseModal, submitImport, busy, submitting, fillHeight = false }: Props) => {
  const [fileList, setFileList] = useState<UploadFile[]>([]);
  const { message } = App.useApp();
  const formatMessage = useTranslate();
  const onSave = async () => {
    const file = fileList[0]?.originFileObj;
    if (!file) {
      message.warning(formatMessage('uns.pleaseUploadTheFile'));
      return;
    }
    if (await submitImport(file)) onCloseModal();
  };

  return (
    <Flex vertical style={{ height: fillHeight ? '100%' : undefined, overflow: 'hidden' }}>
      <div style={{ flex: 1 }}>
        <Dragger
          className={cx('import-upload', fileList.length > 0 && 'upload-file')}
          accept=".json"
          maxCount={1}
          disabled={busy}
          beforeUpload={(file) => {
            if (!file.name.toLowerCase().endsWith('.json')) {
              message.warning(formatMessage('common.theFileFormatType', { fileType: '.json' }));
              return Upload.LIST_IGNORE;
            }
            setFileList([{ uid: file.uid, name: file.name, originFileObj: file }]);
            return false;
          }}
          fileList={fileList}
          onRemove={() => setFileList([])}
        >
          <div className="upload-drag-content">
            <UploadIcon size={32} className="upload-drag-icon" />
            <p className="upload-hint-primary">{formatMessage('common.clickOrDragForUpload')}</p>
          </div>
        </Dragger>
      </div>
      <Divider style={{ margin: '16px 0' }} />
      <Flex align="center" gap={8} justify="flex-end">
        <ComButton onClick={onCloseModal}>{formatMessage('common.cancel')}</ComButton>
        <ComButton type="primary" onClick={onSave} loading={submitting} disabled={busy}>
          {formatMessage('common.import')}
        </ComButton>
      </Flex>
    </Flex>
  );
};

export default ImportDom;
