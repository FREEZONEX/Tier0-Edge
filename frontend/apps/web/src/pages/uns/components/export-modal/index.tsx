import type { Dispatch, RefObject, SetStateAction } from 'react';
import { type FC, useImperativeHandle, useState } from 'react';
import { App, Divider, Flex } from 'antd';
import { useTranslate } from '@/hooks';
import ProModal from '@/components/pro-modal';
import { UnsTree } from '@/pages/uns/components/export-modal/uns-tree.tsx';
import styles from './index.module.scss';
import { TreeStoreProvider, useTreeStore } from '@/pages/uns/components/export-modal/treeStore.tsx';
import { CodeDom } from '@/pages/uns/components/export-modal/code-dom.tsx';
import ComButton from '../../../../components/com-button';
import { processedCheckedKeys } from '@/pages/uns/store/utils.ts';
import { getParamsForArray } from '@/utils';
import { exportExcelGlobal } from '@/apis/core-api';
import { useDownloadNotification } from '@/hooks/useDownloadNotification';

interface ExportModalRef {
  setOpen: Dispatch<SetStateAction<boolean>>;
}

export interface ExportModalProps {
  exportRef?: RefObject<ExportModalRef>;
}

const Content = ({ isFullscreen, open, onClose }: { isFullscreen?: boolean; open: boolean; onClose: () => void }) => {
  const formatMessage = useTranslate();
  const { checkedKeys, allChecked, treeData } = useTreeStore((state) => ({
    checkedKeys: state.checkedKeys,
    allChecked: state.allChecked,
    treeData: state.treeData,
  }));
  const [showDownloadNotification, contextHolder] = useDownloadNotification();
  const { message } = App.useApp();

  const onExport = async () => {
    const params: any = {};
    if (!(allChecked || checkedKeys?.length)) {
      message.warning(formatMessage('home.mustOne'));
      return;
    }
    if (allChecked) {
      params['unsExportParam'] = {
        exportType: 'ALL',
      };
    } else {
      // 根据checkedKeys匹配节点信息
      const matchedNodes = processedCheckedKeys({
        checkedKeys,
        strategy: 'SHOW_PARENT',
        treeData, // 添加treeData参数
      });
      params['unsExportParam'] = {
        ...getParamsForArray(matchedNodes as any[], 'pathType', {
          groups: {
            0: 'folders',
            2: 'files',
          },
          extract: 'id',
        }),
        checkSmallFile: false,
      };
    }
    return exportExcelGlobal(params).then((zip) => {
      showDownloadNotification({ data: zip, name: 'global-export.zip' });
      message.success(formatMessage('common.optsuccess'));
      onClose();
    });
  };
  const rootStyle = { height: isFullscreen ? '100%' : 600 };

  return (
    <Flex style={rootStyle} vertical>
      {contextHolder}
      <Flex gap={16} style={{ flex: 1, minHeight: 0, overflow: 'hidden' }}>
        <Flex vertical style={{ flex: 1, height: '100%', overflow: 'hidden' }}>
          <Flex className={styles['export-label']}>
            <span>{formatMessage('uns.importUnsTab')}</span>
          </Flex>
          <UnsTree open={open} />
        </Flex>
        <Flex vertical style={{ flex: 1, height: '100%', overflow: 'hidden' }}>
          <Flex className={styles['export-label']}>
            <span>{formatMessage('uns.exportJsonLabel')}</span>
          </Flex>
          <CodeDom />
        </Flex>
      </Flex>

      <div style={{ flexShrink: 0 }}>
        <Divider className={styles.exportFooterDivider} />
        <Flex align="center" gap={8} justify="flex-end">
          <ComButton color="default" variant="filled" onClick={onClose}>
            {formatMessage('common.cancel')}
          </ComButton>
          <ComButton type="primary" onClick={onExport}>
            {formatMessage('common.export')}
          </ComButton>
        </Flex>
      </div>
    </Flex>
  );
};

const Module: FC<ExportModalProps> = (props) => {
  const { exportRef } = props;
  const [open, setOpen] = useState(false);
  const formatMessage = useTranslate();

  const close = () => {
    setOpen(false);
  };
  useImperativeHandle(exportRef, () => ({
    setOpen: setOpen,
  }));

  return (
    <ProModal
      className="exportModalWrap"
      open={open}
      onCancel={close}
      title={
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span>{formatMessage('uns.export')}</span>
        </div>
      }
      width={750}
      maskClosable={false}
      keyboard={false}
      destroyOnHidden
    >
      {(isFullscreen) => {
        return (
          <TreeStoreProvider>
            <Content open={open} isFullscreen={isFullscreen} onClose={close} />
          </TreeStoreProvider>
        );
      }}
    </ProModal>
  );
};
export default Module;
