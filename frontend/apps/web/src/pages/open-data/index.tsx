import { useCallback, useEffect, useState } from 'react';
import { App, Button, Checkbox, Form, Input, Select, Space, Tag } from 'antd';
import { Add, Api, Copy, Edit, TrashCan } from '@carbon/icons-react';
import { useTranslate } from '@/hooks';
import { createSecretKey, deleteSecretKey, querySecretKeyList, updateSecretKey } from '@/apis/core-api/open-data';
import { ButtonPermission } from '@/common-types/button-permission';
import { ComLayout, ComContent, ProTable, ComBtnTabs, AuthWrapper, AuthButton, ProSearch } from '@/components';
import { getApiKeyPermissionOptions, normalizeApiKeyPermission } from '@/components/api-key-permission';
import { MAX_LENGTHS } from '@/utils/limits';
import styles from './index.module.scss';
import { formatTimestamp } from '@/utils';
import { copyToClipboard } from '@/utils/common';
import { createDeleteConfirmOptions } from '@/utils/modal-confirm';
import McpGuide from './mcp-guide';
const I18N_NAME = 'OpenData';

interface dataItemTypes {
  id: number;
  name: string;
  maskedKey: string;
  keyPrefix: string;
  keySuffix: string;
  keyType: string;
  permission: string;
  usageType: string;
  createdTime: number;
  lastUsedTime: number;
  status: number;
}

interface ApiKeyFormValues {
  name: string;
  keyType: string;
  permission: string;
  mcpScopes?: string[];
}

const apiKeyTypeOptions = ['personal', 'service'];

const permissionLabelKeys = {
  read: 'read_only',
  write: 'data_writer',
  full: 'full_access',
};

const OpenData = () => {
  const formatMessage = useTranslate(I18N_NAME);
  const commonFormatMessage = useTranslate();
  const mcpT = useTranslate('MCP');
  const permissionOptions = getApiKeyPermissionOptions((permission) => formatMessage(permissionLabelKeys[permission]));

  const [dataSource, setDataSource] = useState<dataItemTypes[]>([]);
  const [activeKeyType, setActiveKeyType] = useState('personal');
  const [keyword, setKeyword] = useState('');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [total, setTotal] = useState(0);
  const [form] = Form.useForm<ApiKeyFormValues>();
  const { modal, message } = App.useApp();

  const columns = [
    {
      title: () => formatMessage('name'),
      dataIndex: 'name',
    },
    {
      title: () => formatMessage('permission'),
      dataIndex: 'permission',
      width: '14%',
      render: (txt: string) => (
        <Tag bordered={false} className={styles.permissionTag}>
          {formatMessage(permissionLabelKeys[normalizeApiKeyPermission(txt)])}
        </Tag>
      ),
    },
    {
      title: () => formatMessage('secretKey'),
      dataIndex: 'maskedKey',
      width: '28%',
      minWidth: 260,
      ellipsis: true,
      render: (_: string, record: dataItemTypes) => {
        const maskedKey = getMaskedKey(record);
        return (
          <span className={styles.secretKeyCell} title={maskedKey}>
            {maskedKey}
          </span>
        );
      },
    },
    {
      title: () => formatMessage('creationTime'),
      dataIndex: 'createdTime',
      width: '16%',
      render: (txt: number) => {
        return txt ? <span>{formatTimestamp(txt)}</span> : '-';
      },
    },
    {
      title: () => formatMessage('lastUsed'),
      dataIndex: 'lastUsedTime',
      width: '12%',
      render: (txt: number) => (txt ? <span>{formatTimestamp(txt)}</span> : '-'),
    },
    {
      title: () => formatMessage('operations', undefined, 'Operations'),
      dataIndex: 'operation',
      width: '12%',
      render: (_: any, record: any) => {
        return (
          <Space size={18}>
            <AuthWrapper auth={ButtonPermission['OpenData.enable']}>
              <a className={styles.actionLink} onClick={() => handleEdit(record)}>
                <Edit size={14} /> {formatMessage('edit')}
              </a>
            </AuthWrapper>
            <AuthWrapper auth={ButtonPermission['OpenData.delete']}>
              <a className={`${styles.actionLink} ${styles.deleteActionLink}`} onClick={() => handleDelete(record)}>
                <TrashCan size={14} />
                {formatMessage('delete')}
              </a>
            </AuthWrapper>
          </Space>
        );
      },
    },
  ];

  const getDataSource = useCallback(
    (keyType: string, nextPage = page, nextPageSize = pageSize, nextKeyword = keyword) => {
      querySecretKeyList({ keyType, page: nextPage, size: nextPageSize, keyword: nextKeyword || undefined }).then(
        (data = { list: [], total: 0 }) => {
          setDataSource(data.list || []);
          setTotal(data.total || 0);
        }
      );
    },
    [keyword, page, pageSize]
  );

  useEffect(() => {
    getDataSource(activeKeyType, page, pageSize, keyword);
  }, [activeKeyType, getDataSource, keyword, page, pageSize]);

  const handleAdd = (mcp?: { availableScopes: string[] }) => {
    form.setFieldsValue({
      name: mcp ? mcpT('keyName') : '',
      keyType: activeKeyType,
      permission: 'read',
      mcpScopes: [],
    });
    modal.confirm({
      title: formatMessage('newKey'),
      icon: null,
      closable: true,
      width: 520,
      className: styles.apiKeyFormModal,
      content: renderApiKeyForm(false, mcp),
      okText: commonFormatMessage('common.confirm'),
      cancelText: commonFormatMessage('common.cancel'),
      onOk: async () => {
        const { mcpScopes, ...values } = await form.validateFields();
        const result = await createSecretKey({
          ...values,
          usageType: 'external',
          ...(mcp ? { resourceKeys: ['uns.read', ...(mcpScopes || [])] } : {}),
        });
        message.success(formatMessage('newSuccessfullyAdded'));
        showCreatedKey(result.apiKey, values.keyType);
        if (values.keyType !== activeKeyType) {
          setActiveKeyType(values.keyType);
          setPage(1);
          return;
        }
        getDataSource(values.keyType, 1, pageSize, keyword);
        setPage(1);
      },
    });
  };

  const handleEdit = (record: dataItemTypes) => {
    form.setFieldsValue({
      name: record.name,
      keyType: record.keyType || 'personal',
      permission: normalizeApiKeyPermission(record.permission),
    });
    modal.confirm({
      title: formatMessage('edit'),
      icon: null,
      closable: true,
      width: 520,
      className: styles.apiKeyFormModal,
      content: renderApiKeyForm(true),
      okText: commonFormatMessage('common.confirm'),
      cancelText: commonFormatMessage('common.cancel'),
      onOk: async () => {
        const values = await form.validateFields();
        await updateSecretKey(record.id, { name: values.name, permission: values.permission });
        message.success(formatMessage('updateSuccessfully'));
        getDataSource(activeKeyType, page, pageSize, keyword);
      },
    });
  };

  const handleDelete = (record: dataItemTypes) => {
    modal.confirm({
      ...createDeleteConfirmOptions({
        title: formatMessage('deleteConfirm'),
        name: record?.name,
        formatMessage: commonFormatMessage,
      }),
      onOk: () => {
        return deleteSecretKey(record.id).then(() => {
          message.success(formatMessage('deleteSuccessfully'));
          getDataSource(activeKeyType, page, pageSize, keyword);
        });
      },
    });
  };

  const renderApiKeyForm = (editing: boolean, mcp?: { availableScopes: string[] }) => (
    <Form form={form} layout="vertical" className={styles.apiKeyForm}>
      <Form.Item name="name" label={formatMessage('name')} rules={[{ required: true }]}>
        <Input maxLength={MAX_LENGTHS.apiKeyName} placeholder={formatMessage('enterName')} />
      </Form.Item>
      <Form.Item name="keyType" label={formatMessage('type')} rules={[{ required: true }]}>
        <Select
          disabled={editing}
          options={[
            { label: formatMessage('personal'), value: 'personal' },
            { label: formatMessage('service'), value: 'service' },
          ]}
        />
      </Form.Item>
      <Form.Item name="permission" label={formatMessage('permission')} rules={[{ required: true }]}>
        <Select options={permissionOptions} disabled={!!mcp} />
      </Form.Item>
      {mcp && (
        <Form.Item name="mcpScopes" label={mcpT('extraScopes')} extra={mcpT('scopeHelp')}>
          <Checkbox.Group
            options={[
              {
                label: mcpT('scopeSource'),
                value: 'uns.source',
                disabled: !mcp.availableScopes.includes('uns.source'),
              },
              {
                label: mcpT('scopeStream'),
                value: 'uns.stream',
                disabled: !mcp.availableScopes.includes('uns.stream'),
              },
              {
                label: mcpT('scopeApp'),
                value: 'uns.app.read',
                disabled: !mcp.availableScopes.includes('uns.app.read'),
              },
            ]}
          />
        </Form.Item>
      )}
    </Form>
  );

  const handleCopyCreatedKey = (apiKey: string) => {
    copyToClipboard(apiKey, (success) => {
      if (success) {
        message.success(commonFormatMessage('common.copySuccess'));
        return;
      }
      message.error(commonFormatMessage('common.copyFail'));
    });
  };

  const showCreatedKey = (apiKey: string, keyType?: string) => {
    const title = formatMessage(`${keyType || 'personal'}Key`);

    modal.info({
      icon: null,
      title,
      closable: true,
      footer: null,
      width: 520,
      className: styles.createdKeyModal,
      content: (
        <div className={styles.createdKeyModalContent}>
          <p className={styles.createdKeyDescription}>{formatMessage('createdKey')}</p>
          <div className={styles.createdKeyRow}>
            <Input className={styles.createdKeyInput} value={apiKey} readOnly />
            <Button
              className={styles.createdKeyCopyButton}
              icon={<Copy size={18} />}
              onClick={() => handleCopyCreatedKey(apiKey)}
            />
          </div>
        </div>
      ),
    });
  };

  const handleKeyTypeSelect = (item: any) => {
    setActiveKeyType(item.value);
    setPage(1);
  };

  const handleSearch = (value: string) => {
    setKeyword(value.trim());
    setPage(1);
  };

  const handleCopyText = (text: string) => {
    copyToClipboard(text, (success) => {
      if (success) {
        message.success(commonFormatMessage('common.copySuccess'));
        return;
      }
      message.error(commonFormatMessage('common.copyFail'));
    });
  };

  const renderCodeBox = (text: string) => (
    <div className={styles.codeBox}>
      <pre>{text}</pre>
      <Button type="text" icon={<Copy size={16} />} onClick={() => handleCopyText(text)} />
    </div>
  );

  const apiExample = `curl -X POST "${window.location.origin}/openapi/v1/uns/browse" \\
-H "Content-Type: application/json" \\
-H "x-api-key: <API_KEY>" \\
-d '{"path":"/"}'`;

  const cliInstallCommand = 'npx @tier0/cli@latest install';
  const cliAgentPrompt =
    'help me to install tier0 cli : https://raw.githubusercontent.com/FREEZONEX/Tier0-skill/main/README.md';

  const renderAccessCards = () => (
    <div className={styles.accessCards}>
      <section className={styles.accessCard}>
        <div className={styles.accessCardTitle}>
          <span className={styles.accessIcon}>
            <Api size={20} />
          </span>
          <strong>{formatMessage('apiKeyAccess')}</strong>
        </div>
        <p>{formatMessage('apiKeyAccessDesc')}</p>
        {renderCodeBox(apiExample)}
        <Button type="link" className={styles.apiListLink} onClick={() => window.open('/openapi/v1/swagger')}>
          {formatMessage('apiList')}
        </Button>
      </section>
      <section className={styles.accessCard}>
        <div className={styles.accessCardTitle}>
          <span className={styles.accessIconText}>KEY</span>
          <strong>{formatMessage('cliAccess')}</strong>
        </div>
        <p>{formatMessage('cliAccessDesc')}</p>
        <div className={styles.accessHint}>{formatMessage('cliInstallHint')}</div>
        {renderCodeBox(cliInstallCommand)}
        <div className={styles.accessHint}>{formatMessage('cliAgentPromptHint')}</div>
        {renderCodeBox(cliAgentPrompt)}
      </section>
    </div>
  );

  const getMaskedKey = (record: dataItemTypes) => {
    if (record.maskedKey) {
      return record.maskedKey;
    }
    if (record.keyPrefix || record.keySuffix) {
      return `${record.keyPrefix || ''}******${record.keySuffix || ''}`;
    }
    return '-';
  };

  return (
    <ComLayout loading={false}>
      <ComContent
        hasBack={false}
        mustHasBack={false}
        title={formatMessage('apiKeyManagement')}
        extra={
          <AuthButton
            auth={ButtonPermission['OpenData.addKey']}
            type="primary"
            icon={<Add size={16} />}
            onClick={() => handleAdd()}
          >
            {formatMessage('newKey')}
          </AuthButton>
        }
        style={{
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <div className={styles.tableToolbar}>
          <ComBtnTabs
            className={styles.keyTypeTabs}
            activeKey={activeKeyType}
            options={apiKeyTypeOptions.map((item) => ({ label: formatMessage(`${item}Key`), value: item }))}
            onSelect={handleKeyTypeSelect}
          />
          <ProSearch
            className={styles.searchInput}
            size="sm"
            placeholder={formatMessage('searchKeyName')}
            onSearch={handleSearch}
          />
        </div>
        <ProTable
          wrapperStyle={{ flex: '0 0 auto', height: 'auto', minHeight: 320 }}
          columns={columns}
          dataSource={dataSource}
          rowKey="id"
          pagination={{
            current: page,
            pageSize,
            total,
            showSizeChanger: true,
            onChange: (nextPage: number, nextPageSize: number) => {
              setPage(nextPage);
              setPageSize(nextPageSize);
            },
          }}
        />
        <McpGuide onCreateKey={(availableScopes) => handleAdd({ availableScopes })} />
        {renderAccessCards()}
      </ComContent>
    </ComLayout>
  );
};

export default OpenData;
