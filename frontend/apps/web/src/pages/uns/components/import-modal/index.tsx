import { type FC, useImperativeHandle, useState } from 'react';
import { Button, Flex, Tooltip } from 'antd';
import { CircleHelp, ClipboardPaste } from 'lucide-react';
import { useClipboard, useTranslate } from '@/hooks';
import ProModal from '@/components/pro-modal';
import ComRadio from '@/components/com-radio';
import './index.scss';
import ImportDom from '@/pages/uns/components/import-modal/ImportDom.tsx';
import JsonDom from '@/pages/uns/components/import-modal/JsonDom.tsx';
import { Download } from '@/components/lucide-icon/carbon';
import { downloadFn } from '@/utils/blob';
import { template } from './data';
import { agentPrompt } from './agent-prompt';
import { useUnsImportJob } from './useUnsImportJob';

export interface ImportModalProps {
  initTreeData: any;
  importRef: any;
}

const Module: FC<ImportModalProps> = (props) => {
  const { importRef, initTreeData } = props;
  const [open, setOpen] = useState(false);
  const formatMessage = useTranslate();
  const { copy } = useClipboard();
  const [type, setType] = useState('json');
  const { contextHolder, ...importJob } = useUnsImportJob(initTreeData);

  useImperativeHandle(importRef, () => ({
    setOpen: setOpen,
  }));

  const onCloseModal = () => {
    setOpen(false);
    setType('json');
  };

  if (!open) return contextHolder;
  return (
    <>
      {contextHolder}
      <ProModal
        className="importModalWrap"
        open={open}
        onCancel={onCloseModal}
        title={formatMessage('common.import')}
        width={type === 'json' ? 560 : 460}
        maskClosable={false}
        keyboard={false}
        destroyOnHidden
      >
        {(isFullscreen) => {
          const isJsonTab = type === 'json';
          const rootStyle = isFullscreen ? { height: '100%' } : isJsonTab ? { height: 580 } : undefined;
          const panelStyle =
            isFullscreen || isJsonTab ? { flex: 1, minHeight: 0, overflow: 'hidden' as const } : undefined;

          return (
            <Flex vertical style={rootStyle}>
              <Flex className="import-modal-toolbar" align="center" justify="space-between">
                <ComRadio
                  value={type}
                  options={[
                    {
                      value: 'json',
                      label: formatMessage('uns.importUnsTab'),
                    },
                    {
                      value: 'document',
                      label: formatMessage('uns.importAllTab'),
                    },
                  ]}
                  onChange={(e) => {
                    setType(e.target.value);
                  }}
                />
                {type === 'json' && (
                  <Flex className="import-modal-toolbar-actions" align="center" gap={8}>
                    <Button
                      className="import-download-template-btn"
                      icon={<Download size={16} />}
                      onClick={(e) => {
                        e.stopPropagation();
                        downloadFn({ data: template, name: 'uns-template.json' });
                      }}
                    >
                      {formatMessage('common.downloadTemplate')}
                    </Button>
                    <Button
                      className="import-copy-for-agent-btn"
                      onClick={(e) => {
                        e.stopPropagation();
                        copy(agentPrompt);
                      }}
                    >
                      <span className="import-copy-for-agent-content">
                        <ClipboardPaste size={16} />
                        <span>{formatMessage('uns.copyForAgent')}</span>
                        <Tooltip title={formatMessage('uns.copyForAgentTooltip')}>
                          <CircleHelp
                            size={16}
                            className="import-copy-for-agent-help"
                            onClick={(e) => {
                              e.stopPropagation();
                            }}
                          />
                        </Tooltip>
                      </span>
                    </Button>
                  </Flex>
                )}
              </Flex>
              <div style={panelStyle}>
                <div
                  className={isFullscreen ? 'import-document-panel-fill' : undefined}
                  style={{
                    display: type === 'document' ? 'block' : 'none',
                    height: isJsonTab || isFullscreen ? '100%' : 'auto',
                  }}
                >
                  <ImportDom fillHeight={isFullscreen} {...importJob} onCloseModal={onCloseModal} />
                </div>
                <div style={{ display: type === 'json' ? 'block' : 'none', height: '100%' }}>
                  <JsonDom {...importJob} onCloseModal={onCloseModal} />
                </div>
              </div>
            </Flex>
          );
        }}
      </ProModal>
    </>
  );
};
export default Module;
