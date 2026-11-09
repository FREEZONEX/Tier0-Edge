import { type FC, useState, useEffect } from 'react';
import { Form } from 'antd';
import { getTypes } from '@/apis/core-api/uns';
import { useTranslate, useFormValue } from '@/hooks';
import FormItems, { type FormItemType } from './FormItems';
import { uniqBy } from 'lodash-es';
import {
  deriveParentDataTypeFromName,
  getDefaultDataTypeByParent,
  isMultiSegmentName,
  validateNamespaceName,
} from '../path-utils';

import type { FieldItem } from '@/pages/uns/types';
import { useBaseStore } from '@/stores/base';
import { MAX_LENGTHS } from '@/utils/limits';

export interface FormContentProps {
  step: number;
  addNamespaceForAi: { [key: string]: any };
  setAddNamespaceForAi: (e: any) => void;
  open: boolean;
  addModalType: string;
  topicType: number;
}

type GetFormDataParamsType = {
  currentStep: number;
  dataType: number;
  fields: FieldItem[];
  windowType: string;
  addModalType: string;
};
const FormContent: FC<FormContentProps> = ({
  step,
  addNamespaceForAi,
  setAddNamespaceForAi,
  open,
  addModalType,
  topicType,
}) => {
  const form = Form.useFormInstance();
  const formatMessage = useTranslate();
  const { systemInfo, currentUserInfo } = useBaseStore((state) => ({
    systemInfo: state.systemInfo,
    currentUserInfo: state.currentUserInfo,
  }));
  const { enableAutoCategorization, dataQualityEnabled, authEnable } = systemInfo;
  const resourceKeys = (currentUserInfo as (typeof currentUserInfo & { resourceKeys?: string[] }) | undefined)
    ?.resourceKeys;
  const canConfigureDataQuality = Boolean(
    dataQualityEnabled === true &&
    (authEnable === false || currentUserInfo?.superAdmin === true || resourceKeys?.includes('uns.dataQuality.manage'))
  );

  const [types, setTypes] = useState([]);

  const topic = useFormValue('topic', form);
  const path = useFormValue('path', form);
  const name = useFormValue('name', form);
  const calculationType = useFormValue('calculationType', form);
  const dataType = useFormValue('dataType', form);
  const fields = useFormValue('fields', form) || [];
  const windowType = useFormValue(['streamOptions', 'window', 'windowType'], form);
  const parentDataType = useFormValue('parentDataType', form) || topicType || 1;

  const isCreateFolder = addModalType.includes('Folder');
  const isFormTopic = addModalType.includes('topic');
  const isStandardCreate = addModalType === 'addFolder' || addModalType === 'addFile';
  const derivedParentDataType = deriveParentDataTypeFromName(name);
  const lockParentDataType =
    enableAutoCategorization && addModalType === 'addFile' && isMultiSegmentName(name) && !!derivedParentDataType;

  useEffect(() => {
    if (!open) return;
    getTypes()
      .then((res: any) => {
        setTypes(res || []);
      })
      .catch((err) => {
        console.log(err);
      });
  }, [open]);

  const selectAll = (options: any[] = []) => {
    const currentReferTopics = form.getFieldValue('referIds') || [];
    const referIds = uniqBy(
      [...currentReferTopics, ...options.map((i) => ({ label: i.path, value: i.id }))].slice(0, 100),
      'value'
    );
    form.setFieldsValue({ referIds });
  };

  const getDataTypeOptions = (parentDataType: number) => {
    if (enableAutoCategorization) {
      switch (parentDataType) {
        case 1:
          return [
            // { label: formatMessage('uns.relational'), value: 2 },
            ...(isFormTopic
              ? []
              : [
                  { label: formatMessage('uns.jsonb'), value: 8 },
                  // { label: formatMessage('uns.aggregation'), value: 6 },
                  // { label: formatMessage('uns.reference'), value: 7 },
                ]),
          ];
        case 2:
          return [{ label: formatMessage('uns.jsonb'), value: 8 }];
        case 3:
          return [
            { label: formatMessage('uns.timeSeries'), value: 1 },
            ...(isFormTopic
              ? []
              : [
                  // { label: formatMessage('uns.calculation'), value: 3 },
                  // { label: formatMessage('uns.aggregation'), value: 6 },
                  // { label: formatMessage('uns.reference'), value: 7 },
                ]),
          ];
      }
    } else {
      return [
        // { label: formatMessage('uns.timeSeries'), value: 1 },
        { label: formatMessage('uns.jsonb'), value: 8 },
        { label: formatMessage('uns.relational'), value: 2 },
        ...(isFormTopic
          ? []
          : [
              // { label: formatMessage('uns.calculation'), value: 3 },
              // { label: formatMessage('uns.aggregation'), value: 6 },
              // { label: formatMessage('uns.reference'), value: 7 },
            ]),
      ];
    }
  };

  const getFormData = (data: GetFormDataParamsType) => {
    const { currentStep, dataType, windowType } = data;

    const formItemList: FormItemType[] = [];

    if (currentStep === 1) {
      if (isFormTopic) {
        formItemList.push({
          formType: 'input',
          formProps: {
            name: 'path',
            label: formatMessage('uns.path'),
          },
          childProps: { disabled: true },
        });
      } else {
        formItemList.push({
          formType: 'input',
          formProps: {
            name: 'topic',
            label: formatMessage('uns.namespace'),
            tooltip: {
              title: formatMessage('uns.namespaceTooltip'),
            },
          },
          childProps: { disabled: true },
        });
      }
      //第一步
      if (isFormTopic) {
        formItemList.push({
          formType: 'input',
          formProps: {
            name: 'topicName',
            label: formatMessage('common.name'),
          },
          childProps: { disabled: true },
        });
      } else {
        formItemList.push({
          formType: 'input',
          formProps: {
            name: 'name',
            label: formatMessage('common.name'),
            rules: [
              { required: true, message: formatMessage('uns.pleaseInputName') },
              {
                validator: (_: any, value: string) => {
                  if (!value) return Promise.resolve();
                  if (isStandardCreate) {
                    const error = validateNamespaceName(value, {
                      isCreateFolder,
                      dataType,
                      parentDataType: topicType,
                    });
                    if (error) {
                      if (error.key === 'uns.namespaceSegmentTooLong') {
                        return Promise.reject(
                          new Error(
                            formatMessage('uns.labelMaxLength', {
                              label: formatMessage('common.name'),
                              length: error.values?.length || MAX_LENGTHS.name,
                            })
                          )
                        );
                      }
                      return Promise.reject(
                        new Error(formatMessage(error.key, error.values ? { ...error.values } : undefined))
                      );
                    }
                    return Promise.resolve();
                  }
                  if (!/^[\u4e00-\u9fa5a-zA-Z0-9_-]+$/.test(value)) {
                    return Promise.reject(new Error(formatMessage('uns.nameFormat')));
                  }
                  if (value.length > MAX_LENGTHS.name) {
                    return Promise.reject(
                      new Error(
                        formatMessage('uns.labelMaxLength', {
                          label: formatMessage('common.name'),
                          length: MAX_LENGTHS.name,
                        })
                      )
                    );
                  }
                  if (isCreateFolder && value === 'label') {
                    return Promise.reject(new Error(formatMessage('uns.prohibitKeywords')));
                  }
                  return Promise.resolve();
                },
              },
            ],
          },
        });
      }

      if (!isCreateFolder) {
        if (enableAutoCategorization) {
          formItemList.push({
            formType: 'radioGroup',
            formProps: {
              name: 'parentDataType',
              label: formatMessage('uns.parentDataType'),
              initialValue: topicType || 1,
              tooltip: {
                title: (
                  <div>
                    <span>{formatMessage('uns.parentDataType')}: </span>
                    <br />
                    <span>• {formatMessage('uns.state')}</span>: "{formatMessage('uns.stateDescription')}"
                    <br />
                    <span>• {formatMessage('uns.action')}</span>: "{formatMessage('uns.actionDescription')}"
                    <br />
                    <span>• {formatMessage('uns.metric')}</span>: "{formatMessage('uns.metricDescription')}"
                    <br />
                    <br />
                    <span>{formatMessage('uns.finalTopicExample')}:</span>
                    <br />
                    <span style={{ wordBreak: 'break-all' }}>• {formatMessage('uns.finalExample')}</span>
                    <br />
                    <br />
                    <span>{formatMessage('uns.briefAnnotation')}:</span>
                    <br />
                    <span>• "{formatMessage('uns.briefAnnotationExample')}"</span>
                    <br />
                  </div>
                ),
              },
            },
            childProps: {
              style: { flexWrap: 'wrap' },
              options: isFormTopic
                ? [
                    { label: formatMessage('uns.state'), value: 1 },
                    { label: formatMessage('uns.metric'), value: 3 },
                  ]
                : [
                    { label: formatMessage('uns.state'), value: 1 },
                    { label: formatMessage('uns.action'), value: 2 },
                    { label: formatMessage('uns.metric'), value: 3 },
                  ],
              disabled: topicType > 0 || lockParentDataType,
              onChange: (e: any) => {
                const resetObj = {
                  refers: [{}],
                  frequency: undefined,
                  aggregationModel: undefined,
                  referIds: undefined,
                  mainKey: undefined,
                  timeReference: undefined,
                };
                const attributeTypeObj = {
                  fields: [{}],
                  attributeType: 1,
                  jsonData: undefined,
                  jsonList: [],
                  jsonDataPath: undefined,
                  source: undefined,
                  dataSource: undefined,
                  table: undefined,
                  next: false,
                };
                switch (e.target.value) {
                  case 1:
                    Object.assign(resetObj, { dataType: 8, ...(dataType !== 1 ? attributeTypeObj : {}) });
                    break;
                  case 2:
                    Object.assign(resetObj, { dataType: 8, ...attributeTypeObj });
                    break;
                  case 3:
                    Object.assign(resetObj, { dataType: 1, ...(dataType !== 2 ? attributeTypeObj : {}) });
                    break;
                }

                form.setFieldsValue(resetObj);
              },
            },
          });
        }
        formItemList.push({
          formType: 'radioGroup',
          formProps: {
            name: 'dataType',
            label: formatMessage('uns.databaseType'),
            initialValue: enableAutoCategorization ? getDefaultDataTypeByParent(topicType || 1) || 8 : 8,
            hidden: !!enableAutoCategorization,
            tooltip: {
              title: (
                <div>
                  <span>• {formatMessage('uns.timeSeries')}</span> — {formatMessage('uns.dataTypeTooltip-TimeSeries')}
                  <br />
                  <span>• {formatMessage('uns.relational')}</span> — {formatMessage('uns.dataTypeTooltip-Relational')}
                  <br />
                  {!isFormTopic && (
                    <>
                      <span>• {formatMessage('uns.calculation')}</span> —&nbsp;
                      {formatMessage('uns.dataTypeTooltip-Calculation')}
                      <br />
                      <span>• {formatMessage('uns.aggregation')}</span> —&nbsp;
                      {formatMessage('uns.dataTypeTooltip-Aggregation')}
                      <br />
                      <span>• {formatMessage('uns.reference')}</span> —&nbsp;
                      {formatMessage('uns.dataTypeTooltip-Reference')}
                    </>
                  )}
                </div>
              ),
            },
          },
          childProps: {
            style: { flexWrap: 'wrap' },
            options: getDataTypeOptions(parentDataType),
            onChange: (e: any) => {
              const resetObj = {
                refers: [{}],
                frequency: undefined,
                aggregationModel: undefined,
                referIds: undefined,
                mainKey: undefined,
                timeReference: undefined,
              };
              if (![1, 2].includes(e.target.value)) {
                Object.assign(resetObj, {
                  fields: [{}],
                  attributeType: 1,
                  jsonData: undefined,
                  jsonList: [],
                  jsonDataPath: undefined,
                  source: undefined,
                  dataSource: undefined,
                  table: undefined,
                  next: false,
                });
              }
              form.setFieldsValue(resetObj);
            },
          },
        });

        if ([1, 2, 8].includes(dataType)) {
          //选择时序或关系型或jsonb
          if (isFormTopic) {
            formItemList.push({
              formType: 'topicToUnsFieldsList',
              formProps: { name: 'topicToUnsFieldsList' },
              childProps: { types },
            });
          } else {
            formItemList.push({
              formType: 'attributeTypeForm',
              formProps: { name: 'attributeTypeForm' },
              childProps: { types, addNamespaceForAi, setAddNamespaceForAi, dataType },
            });
          }
        }

        if (dataType === 3) {
          //选择计算
          formItemList.push(
            {
              formType: 'radioGroup',
              formProps: {
                name: 'calculationType',
                label: formatMessage('uns.calculationType'),
                initialValue: 3,
              },
              childProps: {
                options: [
                  { label: formatMessage('uns.realtime'), value: 3 },
                  // { label: formatMessage('common.history'), value: 4 },
                ],
                onChange: () => {
                  form.setFieldsValue({ fields: [{}] });
                },
              },
            },
            // { formType: 'divider', formProps: { name: 'calculationTypeDivider' } },
            {
              formType: 'fieldsFormList',
              formProps: { name: 'fields' },
              childProps: {
                types,
                addNamespaceForAi,
                setAddNamespaceForAi,
                showMoreBtn: true,
              },
            }
          );
        }
        if (dataType === 6) {
          //选择聚合
          formItemList.push(
            // { formType: 'divider', formProps: { name: 'aggregationDivider' } },
            {
              formType: 'frequency',
              formProps: {
                name: 'frequency',
                label: formatMessage('uns.frequency'),
                required: true,
                tooltip: {
                  title: formatMessage('uns.frequencyTooltip'),
                },
              },
            }
            // {
            //   formType: 'divider',
            //   formProps: {
            //     name: 'frequencyDivider',
            //   },
            // }
          );

          formItemList.push({
            formType: 'searchSelect',
            formProps: {
              name: 'referIds',
              label: formatMessage('uns.aggregationTarget'),
              rules: [{ required: true }],
              tooltip: {
                title: <div>{formatMessage('uns.aggregationTargetTooltip')}</div>,
              },
            },
            childProps: {
              placeholder: formatMessage('uns.searchInstance'),
              mode: 'multiple',
              maxCount: 100,
              selectAll: selectAll,
              apiParams: { type: 2, normal: true },
              labelInValue: true,
            },
          });
        }
        if (dataType === 7) {
          //选择引用
          formItemList.push(
            // { formType: 'divider', formProps: { name: 'referenceDivider' } },
            {
              formType: 'searchSelect',
              formProps: {
                name: 'referId',
                label: formatMessage('uns.referenceTarget'),
                rules: [{ required: true }],
              },
              childProps: {
                placeholder: formatMessage('uns.searchInstance'),
                apiParams: { type: 2, normal: true },
                labelInValue: true,
              },
            }
          );
        }
      }

      formItemList.push({
        formType: 'divider',
        formProps: {
          name: 'aliasDivider',
        },
        childProps: {
          style: {
            marginBottom: 0,
          },
        },
      });
      // collapse
      const collapseFormItemList: FormItemType[] = [];
      collapseFormItemList.push(
        {
          formType: 'input',
          formProps: {
            rules: [{ required: true }, { max: MAX_LENGTHS.alias }],
            name: 'alias',
            label: formatMessage('uns.alias'),
            tooltip: {
              title: formatMessage('uns.aliasTooltip'),
            },
          },
          childProps: { maxLength: MAX_LENGTHS.alias },
        },
        {
          formType: 'input',
          formProps: {
            name: 'displayName',
            label: formatMessage('uns.displayName'),
            rules: [{ max: MAX_LENGTHS.displayName }],
          },
          childProps: { maxLength: MAX_LENGTHS.displayName },
        },
        {
          formType: 'textArea',
          formProps: {
            name: 'description',
            label: formatMessage(isCreateFolder ? 'uns.folderDescription' : 'uns.fileDescription'),
            rules: [
              {
                max: MAX_LENGTHS.description,
                message: formatMessage('uns.labelMaxLength', {
                  label: formatMessage(isCreateFolder ? 'uns.folderDescription' : 'uns.fileDescription'),
                  length: MAX_LENGTHS.description,
                }),
              },
            ],
          },
          childProps: { rows: 2, maxLength: MAX_LENGTHS.description },
        }
      );

      collapseFormItemList.push({ formType: 'expandFormList', formProps: { name: 'expandFormList' } });
      if (isCreateFolder) {
        //创建文件夹
        collapseFormItemList.push({
          formType: 'fieldsFormList',
          formProps: {
            name: 'fields',
          },
          childProps: {
            disabled: false,
            isCreateFolder,
            showMainKey: false,
            types,
            addNamespaceForAi,
            setAddNamespaceForAi,
            style: {
              marginBottom: 16,
            },
          },
        });
      }

      if (addModalType === 'addFile' && canConfigureDataQuality) {
        collapseFormItemList.push({
          formType: 'dataQualityCreate',
          formProps: { name: 'dataQualityCreate' },
        });
      }

      formItemList.push({
        formType: 'collapse',
        formProps: {},
        collapse: {
          key: 'additionalSettings',
          label: formatMessage('uns.additionalSettings'),
          formData: collapseFormItemList,
        },
      });
      if ([1, 2, 6, 8].includes(dataType) && !isCreateFolder) {
        formItemList.push({
          formType: 'divider',
          formProps: { name: 'addFlowDivider' },
          childProps: { style: { marginTop: 0 } },
        });
        const rowFormItemList: FormItemType[] = [];

        if ([1, 2].includes(dataType) && !window.__TIER0_RUNTIME_CONFIG__?.sourceStreamEnabled) {
          rowFormItemList.push({
            formType: 'checkbox',
            formProps: {
              name: 'addFlow',
              initialValue: false,
              valuePropName: 'checked',
              wrapperCol: { span: 22 },
              labelCol: { span: 1 },
              style: {
                marginBottom: 0,
              },
            },
            childProps: {
              children: formatMessage('uns.mockData'),
              tooltip: {
                title: formatMessage('uns.mockDataTooltip'),
              },
              rootClassname: 'opt-checkbox',
            },
          });
        }

        rowFormItemList.push({
          formType: 'checkbox',
          formProps: {
            name: 'persistence',
            initialValue: true,
            valuePropName: 'checked',
            wrapperCol: { span: 22 },
            labelCol: { span: 1 },
            style: {
              marginBottom: 0,
            },
          },
          childProps: {
            children: formatMessage('uns.persistence'),
            tooltip: {
              title: formatMessage('uns.persistenceTooltip'),
            },
            disabled: dataType === 3 && calculationType === 4,
            rootClassname: 'opt-checkbox',
          },
        });

        formItemList.push({
          formType: 'row',
          formProps: {},
          row: {
            key: 'optionalBehaviors',
            label: formatMessage('uns.optionalBehaviors'),
            formData: rowFormItemList,
          },
        });
      }
    }

    if (currentStep === 2) {
      //第二步
      formItemList.push(
        {
          formType: 'showTopic',
          formProps: {
            name: 'showTopic',
            label: formatMessage(isFormTopic ? 'uns.path' : 'uns.namespace'),
            initialValue: isFormTopic ? path : topic,
            tooltip: isFormTopic
              ? undefined
              : {
                  title: formatMessage('uns.namespaceTooltip'),
                },
            style: { marginBottom: 0 },
          },
        },
        {
          formType: 'divider',
          formProps: {
            name: 'topic2Divider',
          },
        }
      );
      if (dataType === 3) {
        if (calculationType === 3) {
          formItemList.push(
            {
              formType: 'expressionForm',
              formProps: { name: 'expressionForm' },
              childProps: {
                showTimeReference: true,
              },
            },
            { formType: 'divider', formProps: { name: 'expressionFormDivider' } }
          );
        }
        if (calculationType === 4) {
          formItemList.push({ formType: 'aggForm', formProps: { name: 'aggForm' } });
        }
      }

      const rowFormItemList: FormItemType[] = [];

      rowFormItemList.push({
        formType: 'checkbox',
        formProps: {
          name: 'persistence',
          initialValue: true,
          valuePropName: 'checked',
          wrapperCol: { span: 22 },
          labelCol: { span: 1 },
          style: {
            marginBottom: 0,
          },
        },
        childProps: {
          children: formatMessage('uns.persistence'),
          tooltip: {
            title: formatMessage('uns.persistenceTooltip'),
          },
          disabled: dataType === 3 && calculationType === 4,
          rootClassname: 'opt-checkbox',
        },
      });

      if (dataType === 3 && calculationType === 4) {
        rowFormItemList.push({
          formType: 'checkbox',
          formProps: {
            name: 'advancedOptions',
            initialValue: false,
            valuePropName: 'checked',
            wrapperCol: { span: 22 },
            labelCol: { span: 1 },
            style: {
              marginBottom: 0,
            },
          },
          childProps: {
            children: formatMessage('streams.advancedOptions'),
            onChange: () => {
              form.setFieldValue('_advancedOptions', undefined);
            },
            disabled: windowType === 'COUNT_WINDOW',
            rootClassname: 'opt-checkbox',
          },
        });
      }

      formItemList.push({
        formType: 'row',
        formProps: {},
        row: {
          key: 'optionalBehaviors2',
          label: formatMessage('uns.optionalBehaviors'),
          formData: rowFormItemList,
        },
      });
    }

    if (currentStep === 3) {
      //第三步
      formItemList.push(
        {
          formType: 'showTopic',
          formProps: {
            name: 'showTopic',
            label: formatMessage('uns.namespace'),
            initialValue: topic,
            tooltip: {
              title: formatMessage('uns.namespaceTooltip'),
            },
            style: { marginBottom: 0 },
          },
        },
        {
          formType: 'divider',
          formProps: {
            name: 'topic3Divider',
          },
        }
      );
      if (dataType === 3 && calculationType === 4) {
        formItemList.push({
          formType: 'advancedOptions',
          formProps: { name: 'advancedOptions' },
        });
      }
    }

    formItemList.push({
      formType: 'divider',
      formProps: {
        name: 'bottomDivider',
      },
      childProps:
        (!isCreateFolder && [1, 2, 6, 8].includes(dataType)) || currentStep === 2
          ? { margin: '16px 0' }
          : { style: { margin: '0 0 16px 0' } },
    });
    return formItemList;
  };
  return (
    <FormItems
      open={open}
      formData={getFormData({
        currentStep: step,
        dataType,
        fields,
        windowType,
        addModalType,
      })}
    />
  );
};

export default FormContent;
