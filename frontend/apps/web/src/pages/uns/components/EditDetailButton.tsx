import { useTranslate } from '@/hooks';
import { useEffect, useMemo, useState } from 'react';
import Icon from '@ant-design/icons';
import { App, Form, Flex, Button } from 'antd';
import ExpandedKeyFormList from '@/pages/uns/components/ExpandedKeyFormList.tsx';
import { modifyDetail, modifyMountedHistory, getInstanceInfo } from '@/apis/core-api/uns.ts';
import { AuthButton } from '@/components/auth';
import OperationForm from '@/components/operation-form';
import ProModal from '@/components/pro-modal';
import FileEdit from '@/components/svg-components/FileEdit';
import { cloneDeep } from 'lodash-es';
import ExpressionForm from '@/pages/uns/components/use-create-modal/components/file/timeSeries/ExpressionForm';
import SearchSelect from '@/pages/uns/components/use-create-modal/components/SearchSelect.tsx';
import { getExpression } from '@/utils/uns';
import { MAX_LENGTHS } from '@/utils/limits';

type ReferType = {
  id: string;
  path: string;
  field: string;
  uts?: boolean;
  variableName: string;
};

type ReferItemType = {
  refer: {
    label: string;
    value: string;
  };
  field: string;
};

const extendToArr = (extend: { [key: string]: string }) => {
  if (!extend) return undefined;
  const arr: { key: string; value: string }[] = [];
  Object.keys(extend).forEach((item) => {
    arr.push({
      key: item,
      value: extend[item],
    });
  });
  return arr;
};

const extendToObj = (extend: { key: string; value: string }[]) => {
  if (!extend) return undefined;
  const obj: { [key: string]: string } = {};
  extend.forEach((item) => {
    obj[item.key] = item.value;
  });
  return obj;
};

const EditDetailButton = ({ auth, type = 'file', modelInfo, getModel }: any) => {
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const [loading, setLoading] = useState(false);
  const [show, setShow] = useState(false);
  const [step, setStep] = useState(1);
  const [form] = Form.useForm();
  const isMountedTopic = type === 'file' && Boolean(modelInfo.mount);

  const scrollToTop = () => {
    const editModalBody = document.querySelector('.editModalBody');
    if (editModalBody) {
      editModalBody.scrollTop = 0;
    }
  };

  const onClose = () => {
    setShow(false);
    setStep(1);
    form.resetFields();
  };

  useEffect(() => {
    if (show)
      setTimeout(() => {
        scrollToTop();
      });
  }, [show, step]);

  const handleBackfill = async () => {
    const { alias, pathName, displayName, description, persistence, extend, refers, expression, dataType } = modelInfo;

    if (isMountedTopic) {
      form.setFieldsValue({ persistence });
      return;
    }

    const backfillForm = {
      alias,
      pathName,
      displayName,
      description,
      persistence,
      extend: extendToArr(extend || []),
    };
    if (type === 'file') {
      if (dataType === 3) {
        //实时计算
        const refersRes = await Promise.all(refers.map((e: ReferType) => getInstanceInfo({ id: e.id })));
        const _refers = refers.map((refer: ReferType, i: number) => ({
          ...refer,
          refer: {
            label: refer.path,
            value: refer.id,
          },
          fields: refersRes[i]?.fields?.filter?.(
            (t: any) => !(t.systemField || ['BLOB', 'LBLOB'].includes(t.type))
          ) || [{ name: refer.field }],
        }));

        Object.assign(backfillForm, {
          refers: _refers,
          expression: getExpression(refers, expression),
        });
      }
      if (dataType === 7) {
        const referId = modelInfo?.refers?.length
          ? {
              label: modelInfo?.refers?.[0]?.path,
              value: modelInfo?.refers?.[0]?.id,
            }
          : undefined;

        Object.assign(backfillForm, {
          referId,
        });
      }
    }

    form.setFieldsValue(backfillForm);
  };

  useEffect(() => {
    if (show) handleBackfill();
  }, [show]);

  const onSave = async () => {
    await form.validateFields();
    const info = cloneDeep(form.getFieldsValue(true));
    const { dataType } = modelInfo;
    const {
      persistence,
      refers,
      expression,

      referId,

      ...restInfo
    } = info;
    if (type === 'file') {
      if (dataType === 3) {
        //实时计算-函数计算
        restInfo.refers = refers.map((item: ReferItemType, index: number) => {
          return {
            id: item?.refer?.value,
            field: item.field,
            variableName: `a${index + 1}`,
            variableGroup: 0,
          };
        });
        restInfo.expression = expression ? expression.replace(/\$(.*?)#/g, '$1') : '';
      }

      if (dataType === 7) {
        //模型
        restInfo.refers = referId?.value ? [{ id: referId.value }] : [];
      }
    }
    setLoading(true);
    const saveRequest = isMountedTopic
      ? modifyMountedHistory({
          id: modelInfo?.id,
          persistence,
        })
      : modifyDetail({
          ...modelInfo,
          ...restInfo,
          id: modelInfo?.id,
          extend: extendToObj(info?.extend),
          persistence: type === 'file' && ![7].includes(dataType) ? persistence : undefined,
        });
    saveRequest
      .then(() => {
        onClose();
        message.success(formatMessage('uns.editSuccessful'));
        getModel?.(info);
      })
      .finally(() => {
        setLoading(false);
      });
  };

  const formItemOptions = useMemo(() => {
    if (isMountedTopic) {
      return [
        {
          type: 'Checkbox',
          name: 'persistence',
          properties: {
            label: formatMessage('uns.persistence'),
            style: { marginLeft: 5 },
          },
          valuePropName: 'checked',
        },
      ];
    }
    switch (step) {
      case 1:
        return [
          {
            label: formatMessage('common.name'),
            name: 'pathName',
            properties: {
              disabled: true,
            },
          },
          {
            label: formatMessage('uns.alias'),
            name: 'alias',
            properties: {
              disabled: true,
            },
          },
          {
            label: formatMessage('uns.displayName'),
            name: 'displayName',
            rules: [{ max: MAX_LENGTHS.displayName }],
            properties: { maxLength: MAX_LENGTHS.displayName },
          },
          {
            type: 'TextArea',
            label: type === 'file' ? formatMessage('uns.fileDescription') : formatMessage('uns.folderDescription'),
            name: 'description',
            rules: [{ max: MAX_LENGTHS.description }],
            properties: { maxLength: MAX_LENGTHS.description },
          },
          {
            component: <SearchSelect apiParams={{ type: 2, normal: true }} labelInValue />,
            label: formatMessage('uns.referenceTarget'),
            name: 'referId',
            noShowKey: type === 'file' && modelInfo.dataType === 7 ? undefined : 'hidden',
          },
          {
            type: 'Checkbox',
            name: 'persistence',
            properties: {
              label: formatMessage('uns.persistence'),
              style: { marginLeft: 5 },
            },
            noShowKey: [7].includes(modelInfo.dataType) && type === 'file' ? 'hidden' : 'folder',
            valuePropName: 'checked',
          },
          {
            type: 'divider',
          },
          {
            render: () => <ExpandedKeyFormList />,
          },
        ]
          .filter((f: any) => (!f.noShowKey || f.noShowKey !== type) && f.noShowKey !== 'hidden')
          .map((e: any) => {
            delete e.noShowKey;
            return e;
          });
      case 2:
        if (type === 'file' && modelInfo.dataType === 3) {
          return [
            {
              render: () => <ExpressionForm apiParams={{ calculationType: 1 }} />,
            },
          ];
        }
        return [];
      default:
        return [];
    }
  }, [type, modelInfo?.dataType, isMountedTopic, modelInfo.calculationType, step]);

  const footer = useMemo(() => {
    return (
      <Flex gap="10px" justify="end">
        {step === 1 ? (
          <Button
            style={{
              height: '40px',
              backgroundColor: 'var(--ui-uns-button-color)',
              color: 'var(--ui-text-color)',
            }}
            color="default"
            variant="filled"
            onClick={onClose}
            block
          >
            {formatMessage('common.cancel')}
          </Button>
        ) : (
          <Button
            style={{
              height: '40px',
              backgroundColor: 'var(--ui-uns-button-color)',
              color: 'var(--ui-text-color)',
            }}
            color="default"
            variant="filled"
            onClick={() => setStep?.(step - 1)}
            disabled={loading}
            block
          >
            {formatMessage('common.prev')}
          </Button>
        )}
        {!isMountedTopic && type === 'file' && modelInfo.dataType === 3 && step === 1 ? (
          <Button style={{ height: '40px' }} type="primary" variant="solid" onClick={() => setStep?.(step + 1)} block>
            {formatMessage('common.next')}
          </Button>
        ) : (
          <Button style={{ height: '40px' }} type="primary" variant="solid" onClick={onSave} loading={loading} block>
            {formatMessage('common.save')}
          </Button>
        )}
      </Flex>
    );
  }, [step, modelInfo?.dataType, loading, getModel, type, isMountedTopic]);

  const renderFrom = useMemo(() => {
    if (!show) return null;
    return (
      <OperationForm
        formConfig={{
          layout: 'vertical',
          labelCol: { span: undefined },
          wrapperCol: { span: undefined },
        }}
        style={{ padding: 0 }}
        form={form}
        formItemOptions={formItemOptions}
        buttonConfig={{ block: true }}
        footer={<span />}
      />
    );
  }, [formItemOptions, show]);

  return (
    <>
      <AuthButton
        auth={auth}
        onClick={() => setShow(true)}
        style={{ border: '1px solid var(--ui-line-color)', background: 'var(--ui-uns-button-color)' }}
        icon={
          <Icon
            data-button-auth={auth}
            component={FileEdit}
            style={{
              fontSize: 16,
              color: 'var(--ui-text-color)',
            }}
          />
        }
      />
      <ProModal
        title={
          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <span>{formatMessage('uns.editDetails')}</span>
          </div>
        }
        onCancel={onClose}
        open={show}
        size="xs"
        afterClose={() => {
          form.resetFields();
        }}
        styles={{
          content: { padding: 0 },
          header: { padding: '20px 24px 10px', margin: 0 },
          body: { padding: '0 24px 0', margin: 0, maxHeight: 'calc(80vh - 122px)', overflowY: 'auto' },
          footer: { padding: '0 24px 20px' },
        }}
        footer={footer}
        classNames={{ body: 'editModalBody' }}
        destroyOnHidden
      >
        {renderFrom}
      </ProModal>
    </>
  );
};

export default EditDetailButton;
