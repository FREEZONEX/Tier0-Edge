import { useState } from 'react';
import { Alert, App, Button, Form, Input, Modal, Space, Table, Typography } from 'antd';
import {
  configureCollaboration,
  createCollaborationEnrollment,
  createCollaborationNode,
  listCollaborationNodes,
  type CollaborationConnection,
  type CollaborationNode,
} from '@/apis/core-api/fleet-acquisition';
import { ButtonPermission } from '@/common-types/button-permission';
import { AuthButton } from '@/components/auth';
import { useTranslate } from '@/hooks';
import { MAX_LENGTHS } from '@/utils/limits';

export default function FleetCollaborationRegistration({ local = false }: { local?: boolean }) {
  const t = useTranslate('fleet.collaboration');
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [nodes, setNodes] = useState<CollaborationNode[]>([]);
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const [code, setCode] = useState<{ nodeId: string; enrollmentCode: string; expiresAt: number }>();
  const [form] = Form.useForm<CollaborationConnection>();

  const act = async (action: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try {
      await action();
    } catch {
      setError(t('failed'));
    } finally {
      setBusy(false);
    }
  };
  const refresh = async () => setNodes((await listCollaborationNodes()).items);
  const close = () => {
    setOpen(false);
    setCode(undefined);
    form.resetFields();
    setError('');
  };

  return (
    <>
      <AuthButton
        auth={ButtonPermission[local ? 'FleetConnection.configure' : 'FleetNode.add']}
        onClick={() => {
          setOpen(true);
          if (!local) void act(refresh);
        }}
      >
        {t(local ? 'connect' : 'title')}
      </AuthButton>
      <Modal
        title={t(local ? 'connect' : 'title')}
        open={open}
        onCancel={close}
        footer={null}
        width={800}
        destroyOnClose
      >
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Alert type="info" showIcon message={t('networkNotice')} />
          {error && <Alert type="error" showIcon message={error} />}
          {local ? (
            <Form
              form={form}
              layout="vertical"
              onFinish={(values) =>
                void act(async () => {
                  await configureCollaboration(values);
                  form.resetFields(['enrollmentCode']);
                  message.success(t('registered'));
                })
              }
            >
              {(['centerUrl', 'nodeId', 'networkNodeId', 'enrollmentCode'] as const).map((field) => (
                <Form.Item key={field} name={field} label={t(field)} rules={[{ required: true }]}>
                  {field === 'enrollmentCode' ? <Input.Password autoComplete="off" /> : <Input autoComplete="off" />}
                </Form.Item>
              ))}
              <Button htmlType="submit" type="primary" loading={busy}>
                {t('connect')}
              </Button>
            </Form>
          ) : (
            <>
              <Space.Compact block>
                <Input
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  maxLength={MAX_LENGTHS.name}
                  placeholder={t('nodeName')}
                />
                <Button
                  loading={busy}
                  disabled={!name.trim()}
                  onClick={() =>
                    void act(async () => {
                      await createCollaborationNode(name.trim());
                      setName('');
                      await refresh();
                    })
                  }
                >
                  {t('create')}
                </Button>
                <Button disabled={busy} onClick={() => void act(refresh)}>
                  {t('refresh')}
                </Button>
              </Space.Compact>
              <Table<CollaborationNode>
                rowKey="nodeId"
                dataSource={nodes}
                loading={busy}
                pagination={false}
                columns={[
                  { title: t('nodeName'), dataIndex: 'name' },
                  { title: t('nodeId'), dataIndex: 'nodeId', ellipsis: true },
                  {
                    title: t('registration'),
                    render: (_, node) =>
                      t(node.registrationStatus === 'registered' ? 'registeredStatus' : node.registrationStatus),
                  },
                  { title: t('network'), render: (_, node) => t(node.networkStatus) },
                  { title: t('business'), render: (_, node) => t(node.businessStatus) },
                  {
                    title: t('enrollmentCode'),
                    render: (_, node) => (
                      <Button
                        disabled={busy || node.registrationStatus !== 'unregistered'}
                        onClick={() => void act(async () => setCode(await createCollaborationEnrollment(node.nodeId)))}
                      >
                        {t('issueCode')}
                      </Button>
                    ),
                  },
                ]}
              />
              {code && (
                <Alert
                  type="warning"
                  message={t('codeNotice')}
                  description={
                    <Space direction="vertical">
                      <Typography.Text>{code.nodeId}</Typography.Text>
                      <Typography.Text copyable>{code.enrollmentCode}</Typography.Text>
                      <Typography.Text>{new Date(code.expiresAt).toLocaleString()}</Typography.Text>
                    </Space>
                  }
                />
              )}
            </>
          )}
        </Space>
      </Modal>
    </>
  );
}
