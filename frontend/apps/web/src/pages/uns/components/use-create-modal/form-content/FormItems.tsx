import type { FC } from 'react';
import { Form, Input, Select, Divider, Collapse, Row, Col } from 'antd';
import type { FormItemProps } from 'antd';
import SearchSelect from '@/pages/uns/components/use-create-modal/components/SearchSelect';
import FieldsFormList from '@/pages/uns/components/use-create-modal/components/FieldsFormList';
import ReverseGeneration from '@/pages/uns/components/use-create-modal/components/file/ReverseGeneration';
import TopicToUnsFieldsList from '@/pages/uns/components/use-create-modal/components/file/TopicToUnsFieldsList';
import FrequencyForm from '@/pages/uns/components/use-create-modal/components/file/FrequencyForm';
import ExpressionForm from '@/pages/uns/components/use-create-modal/components/file/timeSeries/ExpressionForm';
import AggForm from '@/pages/uns/components/use-create-modal/components/file/timeSeries/AggForm';
import AdvancedOptions from '@/pages/uns/components/use-create-modal/components/file/AdvancedOptions';
import ExpandedKeyFormList from '@/pages/uns/components/ExpandedKeyFormList';
import ComCheckbox from '@/components/com-checkbox';
import ComRadio from '@/components/com-radio';
import AttributeTypeForm from '@/pages/uns/components/use-create-modal/components/file/AttributeTypeForm';
import { CaretRightOutlined } from '@ant-design/icons';
import styles from './index.module.scss';
import DataQualityCreateSettings from '@/pages/uns/components/use-create-modal/components/DataQualityCreateSettings';

const { TextArea } = Input;

export interface FormItemType {
  // collapse | formType | optBehaviors
  formType: string | 'collapse' | 'row';
  formProps: FormItemProps;
  childProps?: { [key: string]: any };
  // collapse
  collapse?: {
    formData: FormItemType[];
    key: string;
    label: string;
  };
  // optBehaviors
  row?: {
    formData: FormItemType[];
    key: string;
    label: string;
  };
}

export interface FormItemsProps {
  formData: FormItemType[];
  open: boolean;
}

const renderItems = (item: FormItemType) => {
  const { formType, formProps = {}, childProps = {} } = item;
  const key = formProps.name;
  switch (formType) {
    case 'showTopic':
      return (
        <Form.Item {...formProps} key={key}>
          <div className="namespaceValue">{formProps.initialValue}</div>
        </Form.Item>
      );
    case 'divider':
      return (
        <Divider style={{ borderColor: 'var(--ui-line-color)', margin: '16px 0', ...childProps?.style }} key={key} />
      );
    case 'input':
      return (
        <Form.Item {...formProps} key={key}>
          <Input {...childProps} />
        </Form.Item>
      );
    case 'textArea':
      return (
        <Form.Item {...formProps} key={key}>
          <TextArea {...childProps} />
        </Form.Item>
      );
    case 'select':
      return (
        <Form.Item {...formProps} key={key}>
          <Select {...childProps} />
        </Form.Item>
      );
    case 'searchSelect':
      return (
        <Form.Item {...formProps} key={key}>
          <SearchSelect {...childProps} />
        </Form.Item>
      );
    case 'radioGroup':
      return (
        <Form.Item {...formProps} key={key}>
          <ComRadio {...childProps} />
        </Form.Item>
      );
    case 'checkbox':
      return (
        <Form.Item {...formProps} key={key}>
          <ComCheckbox {...childProps} />
        </Form.Item>
      );
    case 'frequency':
      return (
        <Form.Item {...formProps} key={key}>
          <FrequencyForm {...childProps} />
        </Form.Item>
      );
    case 'expandFormList':
      return <ExpandedKeyFormList key={key} />;
    case 'fieldsFormList':
      return <FieldsFormList {...childProps} key={key} />;
    case 'reverseGeneration':
      return <ReverseGeneration {...childProps} key={key} />;
    case 'topicToUnsFieldsList':
      return <TopicToUnsFieldsList {...childProps} key={key} />;
    case 'expressionForm':
      return <ExpressionForm key={key} {...childProps} />;
    case 'aggForm':
      return <AggForm key={key} />;
    case 'advancedOptions':
      return <AdvancedOptions key={key} />;
    case 'attributeTypeForm':
      return <AttributeTypeForm key={key} {...childProps} />;
    case 'dataQualityCreate':
      return <DataQualityCreateSettings key={key} />;
    default:
      return null;
  }
};
const FormItems: FC<FormItemsProps> = ({ formData, open }) => {
  return (
    <>
      {formData.map((item: FormItemType) => {
        if (item.formType === 'row' && item.row) {
          const { key, label, formData } = item.row;

          return (
            <div key={key}>
              <Row style={{ marginBottom: 8 }}>{label}</Row>
              <Row>
                {formData?.map((item: FormItemType) => {
                  return (
                    <Col key={item.formProps.name} span={24 / (formData.length || 1)}>
                      {renderItems(item)}
                    </Col>
                  );
                })}
              </Row>
            </div>
          );
        }
        if (item.formType === 'collapse' && item.collapse) {
          const { key, label, formData } = item.collapse;
          const items = [
            {
              key,
              label,
              children: (
                <>
                  {formData?.map((item: FormItemType) => {
                    return renderItems(item);
                  })}
                </>
              ),
            },
          ];
          return (
            <Collapse
              key={key + 'collapse' + open}
              rootClassName={styles['custom-collapse']}
              ghost
              bordered={false}
              items={items}
              expandIcon={({ isActive }) => <CaretRightOutlined rotate={isActive ? 90 : 0} />}
            />
          );
        }
        return renderItems(item);
      })}
    </>
  );
};

export default FormItems;
