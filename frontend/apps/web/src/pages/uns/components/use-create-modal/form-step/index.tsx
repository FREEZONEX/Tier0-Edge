import { type FC, useState, type Dispatch, type SetStateAction, useEffect } from 'react';
import { ChevronRight, ChevronLeft } from '@/components/lucide-icon/carbon';
import { Form, App, Button, Flex } from 'antd';
import { addModel, pasteUns } from '@/apis/core-api/uns';
import { useTranslate, useFormValue } from '@/hooks';
import dayjs from 'dayjs';
import { cloneDeep } from 'lodash-es';
import type { FieldItem, UnsTreeNode } from '@/pages/uns/types';
import { ROOT_NODE_ID } from '../../../store/treeStore';
import type { TreeStoreActions } from '../../../store/types';
import { getTargetNode } from '@/utils/uns';
import ComPopupGuide from '@/components/com-popup-guide';
import { useTreeStore } from '@/pages/uns/store/treeStore';
import { deriveParentDataTypeFromName, isMultiSegmentName } from '../path-utils';

export interface FormStepProps {
  step: number;
  setStep: Dispatch<SetStateAction<number>>;
  handleClose: (cb?: () => void) => void;
  isCreateFolder: boolean;
  addNamespaceForAi: { [key: string]: any };
  setAddNamespaceForAi: (e: any) => void;
  successCallBack: TreeStoreActions['loadData'];
  changeCurrentPath: (node: UnsTreeNode) => void;
  setTreeMap: TreeStoreActions['setTreeMap'];
  sourceId: string;
  addModalType: string;
}

interface DataQualityRangeDraft {
  fieldName?: string;
  lowerLimit?: number;
  upperLimit?: number;
  unit?: string;
}

const nonZeroNumber = (value: unknown) =>
  typeof value === 'number' && Number.isFinite(value) && value !== 0 ? value : undefined;

const millisecondsFromSeconds = (value: unknown) => {
  const seconds = Number(value || 0);
  return Number.isFinite(seconds) && seconds > 0 ? Math.round(seconds * 1000) : 0;
};

const applyDataQualityRanges = (fields: FieldItem[], ranges?: DataQualityRangeDraft[]) => {
  const rangeByField = new Map(
    (ranges || []).filter((range) => range?.fieldName).map((range) => [String(range.fieldName), range] as const)
  );
  return fields.map((field) => {
    const range = rangeByField.get(field.name);
    if (!range) return field;
    return {
      ...field,
      unit: range.unit?.trim() || field.unit,
      lowerLimit: nonZeroNumber(range.lowerLimit),
      upperLimit: nonZeroNumber(range.upperLimit),
    };
  });
};

const FormStep: FC<FormStepProps> = ({
  step,
  setStep,
  handleClose,
  isCreateFolder,
  addNamespaceForAi,
  setAddNamespaceForAi,
  successCallBack,
  changeCurrentPath,
  setTreeMap,
  sourceId,
  addModalType,
}) => {
  const { message } = App.useApp();
  const formatMessage = useTranslate();
  const form = Form.useFormInstance();
  const [loading, setLoading] = useState(false);

  const { lazyTree, treeData } = useTreeStore((state) => ({
    lazyTree: state.lazyTree,
    treeData: state.treeData,
  }));

  //以下变量用于控制步骤按钮的显示
  const advancedOptions = useFormValue('advancedOptions', form) || false;
  const calculationType = useFormValue('calculationType', form);
  const _dataType = useFormValue('dataType', form);
  const attributeType = useFormValue('attributeType', form);
  const jsonList = useFormValue('jsonList', form);

  const isFormTopic = addModalType.includes('topic');
  const isStandardCreateFile = addModalType === 'addFile';

  const extendToObj = (extend: { key: string; value: string }[]) => {
    if (!extend) return undefined;
    const obj: { [key: string]: string } = {};
    extend.forEach((item) => {
      obj[item.key] = item.value;
    });
    return obj;
  };

  const hasFieldDefinition = (field?: Partial<FieldItem>) => {
    if (!field) return false;
    return [field.name, field.type, field.unit].some(
      (value) => value !== undefined && value !== null && String(value).trim() !== ''
    );
  };

  const normalizeSchemaField = (field: FieldItem) => ({
    name: field.name,
    type: field.type,
    unit: field.unit,
    unique: field.unique,
    lowerLimit: nonZeroNumber(field.lowerLimit),
    upperLimit: nonZeroNumber(field.upperLimit),
  });

  const normalizeSchemaFields = (fieldItems?: FieldItem[]) =>
    Array.isArray(fieldItems) ? fieldItems.filter(hasFieldDefinition).map(normalizeSchemaField) : [];

  const save = () => {
    form
      .validateFields()
      .then(async () => {
        const next = form.getFieldValue('next');
        if ((attributeType === 3 || (isFormTopic && jsonList?.length > 1)) && _dataType !== 8 && !next) {
          message.error(formatMessage('uns.noFieldsTip'));
          return;
        }
        const {
          alias,
          fields,
          dataType,
          description,
          extend,
          addFlow,
          persistence,
          calculationType,
          refers,
          expression,
          mainKey,
          frequency,
          referIds,
          referId,
          name,
          displayName,
          timeReference,

          functions,
          DataSource,
          streamOptions,
          whereCondition,
          havingCondition,
          advancedOptions,

          _advancedOptions,
          table,

          pasteInfo,
          parentDataType,
          pasteNode,
          dataQualityRanges,
          dataQualityExpectedSeconds,
          dataQualityGraceSeconds,
        } = cloneDeep(form.getFieldsValue(true));
        const fieldsWithQualityRanges = applyDataQualityRanges(fields || [], dataQualityRanges);
        const derivedParentDataType = isStandardCreateFile ? deriveParentDataTypeFromName(name) : undefined;
        const isAbsoluteNamespacePath = !isFormTopic && isMultiSegmentName(name);
        // 表单验证通过后的操作
        const data: { [key: string]: any } = isCreateFolder
          ? {
              name,
              displayName,
              parentId: isAbsoluteNamespacePath ? undefined : sourceId || undefined,
              alias,
              description,
              fields,
              pathType: 0,
              extend: extendToObj(extend),
            }
          : {
              name,
              displayName,
              parentId: isAbsoluteNamespacePath ? undefined : sourceId || undefined,
              alias,
              dataType,
              description,
              persistence,
              pathType: 2,
              extend: extendToObj(extend),
              fields: [1, 2, 3, 8].includes(dataType) ? fieldsWithQualityRanges : undefined,
              parentDataType: derivedParentDataType || parentDataType,
            };

        if (!isCreateFolder) {
          switch (dataType) {
            case 1:
            case 2:
              if (dataType === 1) {
                data.fields = normalizeSchemaFields(fieldsWithQualityRanges.filter((e: FieldItem) => !e.systemField));
              } else {
                if (mainKey > -1) fieldsWithQualityRanges[mainKey].unique = true;
                data.fields = normalizeSchemaFields(fieldsWithQualityRanges);
              }
              data.addFlow = addFlow;
              if (table?.value) {
                data.protocol = {
                  referenceDataSource: table.value
                    ?.split?.('$分隔符$')
                    ?.filter((e: string) => e !== 'tables')
                    ?.join('.'),
                };
              }
              break;
            case 3:
              if (calculationType === 3) {
                data.fields = normalizeSchemaFields(fieldsWithQualityRanges.filter((e: FieldItem) => !e.systemField));

                type ReferItemType = {
                  refer: {
                    label: string;
                    value: string;
                  };
                  field: string;
                };
                //实时计算
                data.refers = refers.map((item: ReferItemType) => {
                  return { id: item?.refer?.value, field: item.field, uts: item?.refer?.value === timeReference };
                });
                data.expression = expression ? expression.replace(/\$(.*?)#/g, '$1') : '';
              }
              if (calculationType === 4) {
                //历史计算
                data.dataType = 4;

                data.referTopic = DataSource.value;
                if (functions && Array.isArray(functions) && fields && Array.isArray(fields)) {
                  data.fields = fields.map((field: FieldItem, index: number) => {
                    const func = functions[index];
                    return {
                      ...field,
                      index: `${func.functionType}(${func.key})`,
                    };
                  });
                }

                if (whereCondition) streamOptions.whereCondition = whereCondition.replace(/\$(.*?)#/g, '$1');
                if (havingCondition) streamOptions.havingCondition = havingCondition.replace(/\$(.*?)#/g, '$1');

                if (advancedOptions && _advancedOptions) {
                  //高级流选项
                  if (_advancedOptions.trigger === 'MAX_DELAY') {
                    _advancedOptions.trigger = `MAX_DELAY ${_advancedOptions.delayTime}`;
                    delete _advancedOptions.delayTime;
                  }
                  if (_advancedOptions.startTime)
                    _advancedOptions.startTime = dayjs(_advancedOptions.startTime).format('YYYY-MM-DD');
                  if (_advancedOptions.endTime)
                    _advancedOptions.endTime = dayjs(_advancedOptions.endTime).format('YYYY-MM-DD');
                  Object.keys(_advancedOptions).forEach((key: string) => {
                    if (['', undefined, null].includes(_advancedOptions[key])) delete _advancedOptions[key];
                  });
                }
                data.streamOptions = { ...streamOptions, ..._advancedOptions };
              }
              break;
            case 6:
              Object.assign(data, {
                frequency: frequency.value + frequency.unit,
                referIds: referIds.map((e: { value: string }) => e.value),
              });
              break;
            case 7:
              Object.assign(data, {
                referIds: [referId?.value],
                persistence: undefined,
              });
              break;
            case 8:
              Object.assign(data, {
                fields: normalizeSchemaFields(fieldsWithQualityRanges),
              });
              break;
            default:
              break;
          }
        }

        if (!isCreateFolder) {
          const expectedIntervalMs = millisecondsFromSeconds(dataQualityExpectedSeconds);
          const missingGraceMs = millisecondsFromSeconds(dataQualityGraceSeconds);
          const hasValueRange = fieldsWithQualityRanges.some(
            (field) => nonZeroNumber(field.lowerLimit) !== undefined || nonZeroNumber(field.upperLimit) !== undefined
          );
          if (expectedIntervalMs > 0 || missingGraceMs > 0 || hasValueRange) {
            data.dataQualityPolicy = {
              notifyOnIssue: false,
              expectedIntervalMs,
              missingGraceMs,
            };
          }
        }

        setLoading(true);
        const requestSourceId = isAbsoluteNamespacePath ? '' : sourceId;
        const handleCallback = (data: { id: string; parentId: string }, queryType: string) => {
          const { id, parentId } = data;
          const hasParentNode = getTargetNode(treeData || [], parentId);

          const _parentId = hasParentNode ? parentId : requestSourceId ? requestSourceId : ROOT_NODE_ID;
          const _childId = hasParentNode || parentId === requestSourceId || !lazyTree ? id : parentId;

          successCallBack(
            {
              queryType,
              key: _parentId,
              newNodeKey: _childId,
              reset: isAbsoluteNamespacePath || !(requestSourceId || parentId),
              nodeDetail: pasteNode,
            },
            (_, selectInfo, opt) => {
              const currentNode = getTargetNode(_ || [], _childId);
              if (!selectInfo && !_childId) return;

              changeCurrentPath(
                selectInfo ||
                  currentNode || { key: _childId, id: _childId, pathType: queryType === 'addFolder' ? 0 : 2 }
              );
              setTreeMap(false);
              if (selectInfo) {
                // 非lasy树
                opt?.scrollTreeNode?.(id);
              }
            }
          );
        };
        if (pasteInfo) {
          pasteUns({
            sourceId: pasteInfo?.sourceId || undefined,
            targetId: data?.parentId || undefined,
            newF: data,
          })
            .then(({ msg, code, data }) => {
              handleCallback(data, isCreateFolder ? 'addFolder' : 'addFile');
              handleClose(() => setLoading(false));
              if (code === 206) {
                message.warning(msg);
              } else {
                message.success(formatMessage('uns.pasteSuccess'));
              }
            })
            .catch(() => {
              setLoading(false);
            });
        } else {
          const finalData =
            data?.dataType === 2
              ? {
                  ...data,
                  fields: data.fields,
                }
              : data;
          addModel(finalData)
            .then((res: any) => {
              message.success(formatMessage('uns.newSuccessfullyAdded'));
              handleCallback(res, isCreateFolder ? 'addFolder' : 'addFile');
              handleClose(() => setLoading(false));
            })
            .catch((err) => {
              setLoading(false);
              console.error(err);
              setAddNamespaceForAi?.(null);
            })
            .finally(() => {
              if (isCreateFolder && addNamespaceForAi) {
                // 如果是新增文件的
                setTimeout(() => {
                  setAddNamespaceForAi({ ...addNamespaceForAi, currentStep: 'openFileNewModal' });
                }, 500);
              }
            });
        }
      })
      .catch(() => undefined)
      .finally(() => {
        if (addNamespaceForAi) {
          setAddNamespaceForAi?.(null);
        }
      });
  };

  const handleStep = async () => {
    return form.validateFields().then(() => {
      if (step === 2) {
        if (calculationType === 4 && advancedOptions) {
          setStep(() => step + 1);
        }
      } else {
        setStep(() => step + 1);
      }
    });
  };

  useEffect(() => {
    const drawerBody = document.querySelector('.newFolderOrFileModalBody');
    if (drawerBody) drawerBody.scrollTop = 0;
  }, [step]);

  return (
    <Flex align="center" justify="flex-end" gap={10}>
      {step > 1 && (
        <Button
          color="default"
          variant="filled"
          style={{ color: 'var(--ui-text-color)', backgroundColor: 'var(--ui-uns-button-color)' }}
          icon={<ChevronLeft />}
          onClick={() => {
            setStep(() => step - 1);
          }}
          disabled={loading}
        >
          {formatMessage('common.prev')}
        </Button>
      )}
      {(step === 1 && [1, 2, 6, 7, 8].includes(_dataType)) ||
      (step === 2 && !advancedOptions) ||
      step === 3 ||
      isCreateFolder ? (
        <ComPopupGuide
          key={isCreateFolder ? 'saveFolder' : 'saveFile'}
          currentStep={addNamespaceForAi?.currentStep}
          stepName={isCreateFolder ? 'saveFolder' : 'saveFile'}
          steps={addNamespaceForAi?.steps}
          placement="left"
          onFinish={() => {
            save?.();
          }}
        >
          <Button color="primary" variant="solid" onClick={save} loading={loading}>
            {formatMessage('common.save')}
          </Button>
        </ComPopupGuide>
      ) : (
        <ComPopupGuide
          stepName={`next`}
          steps={addNamespaceForAi?.steps}
          currentStep={addNamespaceForAi?.currentStep}
          onFinish={(_, nextStepName) => {
            handleStep()
              .then(() => {
                setAddNamespaceForAi({
                  ...addNamespaceForAi,
                  currentStep: nextStepName,
                });
              })
              .catch(() => {
                setAddNamespaceForAi(null);
              });
          }}
        >
          <Button color="default" variant="filled" icon={<ChevronRight />} iconPosition="end" onClick={handleStep}>
            {formatMessage('common.next')}
          </Button>
        </ComPopupGuide>
      )}
    </Flex>
  );
};
export default FormStep;
