import { Flex, message, Upload as AntUpload } from 'antd';
import { useTranslate } from '@/hooks';
import { Upload as UploadIcon } from '@/components/lucide-icon/carbon';
import { type MouseEvent, useRef, useState } from 'react';
import { uploadAttachment } from '@/apis/core-api/attachments.ts';
import { AuthButton } from '@/components/auth';
import ProModal from '@/components/pro-modal';
import ComButton from '@/components/com-button';
import '@/pages/uns/components/import-modal/index.scss';

const { Dragger } = AntUpload;

const UploadButton = ({
  alias,
  ownerId,
  documentListRef,
  auth,
  setActiveList,
}: {
  auth?: string;
  alias: string;
  ownerId?: string | number;
  documentListRef: any;
  setActiveList?: any;
}) => {
  const formatMessage = useTranslate();
  const [loading, setLoading] = useState(false);
  const [fileList, setFileList] = useState<any[]>([]);
  const [show, setShow] = useState(false);
  const uploadRootRef = useRef<HTMLDivElement>(null);
  const openFileDialog = (event?: MouseEvent<HTMLElement>) => {
    event?.stopPropagation();
    const target = event?.target as HTMLElement | null;
    if (target?.tagName === 'INPUT') return;
    if (target?.closest('.ant-upload-list')) return;
    uploadRootRef.current?.querySelector<HTMLInputElement>('input[type="file"]')?.click();
  };
  const onClose = () => {
    setFileList([]);
    setShow(false);
  };
  const beforeUpload = (file: any) => {
    if (file.size <= 1024 * 1024 * 10) {
      setFileList((pre) => {
        return [...pre, file];
      });
    } else {
      message.warning(formatMessage('uns.importDocumentMax'));
    }
    return false;
  };

  const onSave = () => {
    if (!fileList.length) {
      message.warning(formatMessage('uns.importDocumentSelect', {}, 'Please select a file'));
      return;
    }
    setLoading(true);
    uploadAttachment(
      fileList?.map((item: any) => ({ value: item, name: 'files', fileName: item.name })),
      { alias, ownerId }
    )
      .then(() => {
        documentListRef?.current?.refresh?.();
        message.success(formatMessage('common.optsuccess'));
        onClose();
        setActiveList?.((pre: string[]) => {
          return [...new Set([...(pre || []), 'document'])];
        });
      })
      .finally(() => {
        setLoading(false);
      });
  };
  return (
    <>
      <AuthButton
        auth={auth}
        onClick={() => setShow(true)}
        style={{ border: '1px solid var(--ui-line-color)', background: 'var(--ui-uns-button-color)' }}
        icon={<UploadIcon size={17} />}
      >
        {formatMessage('common.upload')}
      </AuthButton>
      <ProModal
        aria-label=""
        title={formatMessage('uns.importDocument')}
        onCancel={onClose}
        open={show}
        className="importModalWrap attachment-upload-modal"
        width={520}
      >
        <div ref={uploadRootRef} onClick={openFileDialog}>
          <Dragger
            className="uploadWrap"
            action=""
            multiple
            fileList={fileList}
            beforeUpload={beforeUpload}
            openFileDialogOnClick={false}
            onRemove={(file) => {
              setFileList(fileList?.filter((item) => item.uid !== file.uid));
            }}
          >
            <div className="upload-drag-content">
              <UploadIcon size={32} className="upload-drag-icon" />
              <p className="upload-hint-primary">
                {formatMessage('uns.importDocumentDragHint', {}, 'Click or drag file to this area to upload')}
              </p>
              <p className="upload-hint-secondary">{formatMessage('uns.importDocumentMax')}</p>
            </div>
          </Dragger>
        </div>
        <Flex className="upload-modal-footer" gap={8} justify="end">
          <ComButton color="default" variant="filled" onClick={onClose} title={formatMessage('common.cancel')}>
            {formatMessage('common.cancel')}
          </ComButton>
          <ComButton
            type="primary"
            variant="solid"
            loading={loading}
            onClick={onSave}
            title={formatMessage('common.save')}
          >
            {formatMessage('common.save')}
          </ComButton>
        </Flex>
      </ProModal>
    </>
  );
};

export default UploadButton;
