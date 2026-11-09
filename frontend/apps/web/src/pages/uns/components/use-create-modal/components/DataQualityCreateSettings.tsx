import { Add } from '@/components/lucide-icon/carbon';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import type { FieldItem } from '@/pages/uns/types';
import { Form, Input, InputNumber, Select, Typography } from 'antd';
import type { FC } from 'react';

import styles from './DataQualityCreateSettings.module.scss';

const numericTypes = new Set(['int', 'integer', 'long', 'bigint', 'float', 'double', 'decimal', 'number']);

const isNumericField = (field: FieldItem) =>
  Boolean(field.name) &&
  !field.systemField &&
  numericTypes.has(
    String(field.type || '')
      .trim()
      .toLowerCase()
  );

const configuredLimit = (value: unknown) =>
  typeof value === 'number' && Number.isFinite(value) && value !== 0 ? value : undefined;

const DataQualityCreateSettings: FC = () => {
  const formatMessage = useTranslate();
  const form = Form.useFormInstance();
  const fields = (Form.useWatch('fields', form) || []) as FieldItem[];
  const ranges = (Form.useWatch('dataQualityRanges', form) || []) as Array<{ fieldName?: string }>;
  const numericFields = fields.filter(isNumericField);
  const fieldByName = new Map(numericFields.map((field) => [field.name, field]));

  return (
    <section className={styles.section} aria-label={formatMessage('dataQuality.title')}>
      <Typography.Title level={5} className={styles.title}>
        {formatMessage('dataQuality.title')}
      </Typography.Title>

      <div className={styles.rule}>
        <div className={styles['setting-title']}>{formatMessage('dataQuality.configCore.valueTitle')}</div>
        <Form.List name="dataQualityRanges" initialValue={[{}]}>
          {(rangeFields, { add }) => {
            const selectedNames = new Set(ranges.map((range) => range?.fieldName).filter(Boolean));
            const canAdd = numericFields.some((field) => !selectedNames.has(field.name));
            const hasEmptyRow = ranges.some((range) => !range?.fieldName);

            return (
              <>
                <div className={styles['range-header']} aria-hidden="true">
                  <span>{formatMessage('dataQuality.range.field')}</span>
                  <span>{formatMessage('dataQuality.range.lower')}</span>
                  <span>{formatMessage('dataQuality.range.upper')}</span>
                  <span>{formatMessage('uns.unit')}</span>
                </div>
                {rangeFields.map(({ key, name }, index) => {
                  const currentName = ranges[index]?.fieldName;
                  const options = numericFields.map((field) => ({
                    value: field.name,
                    label:
                      field.displayName && field.displayName !== field.name
                        ? `${field.displayName} (${field.name})`
                        : field.name,
                    disabled: field.name !== currentName && selectedNames.has(field.name),
                  }));
                  return (
                    <div className={styles['range-row']} key={key}>
                      <Form.Item
                        name={[name, 'fieldName']}
                        rules={[
                          {
                            validator: (_rule, value) => {
                              if (!value) return Promise.resolve();
                              const duplicateCount = ranges.filter((range) => range?.fieldName === value).length;
                              return duplicateCount > 1
                                ? Promise.reject(new Error(formatMessage('dataQuality.range.duplicateField')))
                                : Promise.resolve();
                            },
                          },
                        ]}
                      >
                        <Select
                          allowClear
                          showSearch
                          optionFilterProp="label"
                          options={options}
                          placeholder={formatMessage('dataQuality.range.selectField')}
                          onChange={(fieldName?: string) => {
                            const currentRows = form.getFieldValue('dataQualityRanges') || [];
                            currentRows[name] = {
                              ...currentRows[name],
                              fieldName,
                              unit: fieldName ? fieldByName.get(fieldName)?.unit : undefined,
                            };
                            form.setFieldValue('dataQualityRanges', [...currentRows]);
                          }}
                        />
                      </Form.Item>
                      <Form.Item
                        name={[name, 'lowerLimit']}
                        dependencies={[['dataQualityRanges', name, 'upperLimit']]}
                        rules={[
                          ({ getFieldValue }) => ({
                            validator: (_rule, value) => {
                              const lower = configuredLimit(value);
                              const upper = configuredLimit(getFieldValue(['dataQualityRanges', name, 'upperLimit']));
                              return lower !== undefined && upper !== undefined && lower > upper
                                ? Promise.reject(new Error(formatMessage('dataQuality.range.invalid')))
                                : Promise.resolve();
                            },
                          }),
                        ]}
                      >
                        <InputNumber
                          precision={3}
                          placeholder={formatMessage('dataQuality.range.zeroOrEmpty')}
                          className={styles.control}
                        />
                      </Form.Item>
                      <Form.Item
                        name={[name, 'upperLimit']}
                        dependencies={[['dataQualityRanges', name, 'lowerLimit']]}
                        rules={[
                          ({ getFieldValue }) => ({
                            validator: (_rule, value) => {
                              const upper = configuredLimit(value);
                              const lower = configuredLimit(getFieldValue(['dataQualityRanges', name, 'lowerLimit']));
                              return lower !== undefined && upper !== undefined && lower > upper
                                ? Promise.reject(new Error(formatMessage('dataQuality.range.invalid')))
                                : Promise.resolve();
                            },
                          }),
                        ]}
                      >
                        <InputNumber
                          precision={3}
                          placeholder={formatMessage('dataQuality.range.zeroOrEmpty')}
                          className={styles.control}
                        />
                      </Form.Item>
                      <Form.Item name={[name, 'unit']}>
                        <Input placeholder={formatMessage('dataQuality.range.optional')} />
                      </Form.Item>
                    </div>
                  );
                })}
                <ComButton
                  className={styles['add-field']}
                  type="link"
                  icon={<Add size={16} />}
                  disabled={!canAdd || hasEmptyRow}
                  onClick={() => add({})}
                >
                  {formatMessage('dataQuality.range.addField')}
                </ComButton>
              </>
            );
          }}
        </Form.List>
      </div>

      <div className={styles.rule}>
        <div className={styles['setting-title']}>{formatMessage('dataQuality.configCore.continuityTitle')}</div>
        <div className={styles['continuity-grid']}>
          <Form.Item name="dataQualityExpectedSeconds" label={formatMessage('dataQuality.expectedSeconds')}>
            <InputNumber
              min={0}
              precision={3}
              addonAfter={formatMessage('dataQuality.secondsUnit')}
              placeholder={formatMessage('dataQuality.secondsPlaceholder')}
              className={styles.control}
            />
          </Form.Item>
          <Form.Item name="dataQualityGraceSeconds" label={formatMessage('dataQuality.missingGraceSeconds')}>
            <InputNumber
              min={0}
              precision={3}
              addonAfter={formatMessage('dataQuality.secondsUnit')}
              placeholder={formatMessage('dataQuality.secondsPlaceholder')}
              className={styles.control}
            />
          </Form.Item>
        </div>
        <Typography.Text type="secondary" className={styles.description}>
          {formatMessage('dataQuality.zeroDisablesHelp')}
        </Typography.Text>
      </div>
    </section>
  );
};

export default DataQualityCreateSettings;
