import { type FC, useState } from 'react';
import styles from './FetchData.module.scss';
import { Flex, Tabs } from 'antd';
import { useTranslate } from '@/hooks';
import ComCodeSnippet from '@/components/com-code-snippet';
import ComCopyContent from '@/components/com-copy/ComCopyContent';
import { sqlKeywordsRegex } from '@/utils/pattern';
import { useBaseStore } from '@/stores/base';
import { resolveMqttWebsocketPort, resolveMqttWebsocketUrl } from '@/utils/mqtt';

const apikey = '4174348a-9222-4e81-b33e-5d72d2fd7f1e';

// dataType: 1 时序，2 关系 ， 3 计算
const HistoryData = ({ instanceInfo }: any) => {
  const formatMessage = useTranslate();
  const fields = instanceInfo?.fields?.map((m: any) => m?.name);
  const [tab, setTab] = useState(instanceInfo.dataType !== 2 ? '2' : '1');
  // 关系 - graphQl
  const relationalGraphJs = `curl -X POST -H "Content-Type: application/json" -H "apikey:4174348a-9222-4e81-b33e-5d72d2fd7f1e" -d '{"query":"query MyQuery {${instanceInfo?.alias}(limit: 10, offset: 10) {${fields.join(' \\r\\n ')} \\r\\n}}"}' ${window.location.origin}/open-api/graphql/v1/graphql`;
  // 关系 - restApi
  const relationalRestJsCopy = `curl -H "Accept:application/json" -H "apikey:4174348a-9222-4e81-b33e-5d72d2fd7f1e" ${window.location.origin}/open-api/restapi/api/rest/${instanceInfo?.alias}/10/10`;
  const relationalRestJs = `
  // restapi URL：http://192.168.235.123:8088/open-api/restapi/api/rest/${instanceInfo?.alias}/{offset}/{limit}
  // {offset}:${formatMessage('uns.queryOffset')} {limit}:${formatMessage('uns.queryLimit')}
  curl -H "Accept:application/json" -H "apikey:4174348a-9222-4e81-b33e-5d72d2fd7f1e" ${window.location.origin}/open-api/restapi/api/rest/${instanceInfo?.alias}/10/10
  `;

  // 时序 - restApi
  const timeSeriesRestJs = `curl -X POST -H "Content-Type: application/json" -H "apikey:${apikey}" -d "select ${fields
    ?.map((m: string) => (sqlKeywordsRegex.test(m) ? m : `\`${m}\``))
    .join(',')} from ${instanceInfo?.alias};" ${window.location.origin}/open-api/rest/sql`;
  const items =
    instanceInfo.dataType !== 2
      ? [
          {
            label: 'RestAPI-curl',
            key: '2',
            children: (
              <Flex gap={14} vertical>
                <ComCopyContent
                  label={formatMessage('uns.restApiUrl')}
                  textToCopy={`${window.location.origin}/open-api/rest/sql`}
                />
                <ComCopyContent label={formatMessage('uns.apiKey')} textToCopy={apikey} />
              </Flex>
            ),
          },
        ]
      : [
          {
            label: 'GraphQL-curl',
            key: '1',
            children: (
              <Flex gap={14} vertical>
                <ComCopyContent
                  label={formatMessage('uns.graphqlUrl')}
                  textToCopy={`${window.location.origin}/open-api/graphql/v1/graphql`}
                />
                <ComCopyContent label={formatMessage('uns.apiKey')} textToCopy={apikey} />
              </Flex>
            ),
          },
          {
            label: 'RestAPI-curl',
            key: '2',
            children: (
              <Flex gap={14} vertical>
                <ComCopyContent
                  label={formatMessage('uns.restApiUrl')}
                  textToCopy={`${window.location.origin}/open-api/restapi/api/rest/${instanceInfo?.alias}/10/10`}
                />
                <ComCopyContent label={formatMessage('uns.apiKey')} textToCopy={apikey} />
              </Flex>
            ),
          },
        ];
  return (
    <div className={styles['fetch-data-content']}>
      <div className={styles['fetch-data-info']}>
        <Tabs
          activeKey={tab}
          onChange={(t) => {
            setTab(t);
          }}
          items={items}
        />
      </div>
      <div className={styles['fetch-data-code']}>
        <ComCodeSnippet
          className="codeViewWrap"
          minCollapsedNumberOfRows={24}
          maxCollapsedNumberOfRows={24}
          copyPosition={false}
          copyText={
            instanceInfo.dataType !== 2 ? timeSeriesRestJs : tab === '1' ? relationalGraphJs : relationalRestJsCopy
          }
        >
          {instanceInfo.dataType !== 2 ? timeSeriesRestJs : tab === '1' ? relationalGraphJs : relationalRestJs}
        </ComCodeSnippet>
      </div>
    </div>
  );
};

const RealtimeData = ({ instanceInfo }: any) => {
  const systemInfo = useBaseStore((state) => state.systemInfo);
  const mqttWebsocketUrl = resolveMqttWebsocketUrl(systemInfo);
  const mqttWebsocketPort = resolveMqttWebsocketPort(mqttWebsocketUrl);
  const multipleTopicPre = '';
  const topic = instanceInfo.topic;
  const tcpPort = systemInfo?.mqttTcpPort ?? window.location.port;
  const hostName = window.location.hostname;
  const formatMessage = useTranslate();
  const [tab, setTab] = useState('js');
  const jscode = `
const mqtt = require('mqtt');

const options = {
  clean: true,
  connectTimeout: 4000,
  clientId: 'emqx_test',
  rejectUnauthorized: false,
};

const connectUrl ='${mqttWebsocketUrl}';

const client = mqtt.connect(connectUrl, options);

client.on('connect', function () {
  console.log('Connected');
  client.subscribe('${multipleTopicPre}${topic}', function (err) {
    console.log(err)
  });
});

client.on('message', function (topic, message) {
  console.log(message.toString());
  client.end();
});`;
  const javacode = `
   import org.eclipse.paho.client.mqttv3.*;

   public class MqttDemo {

    public static void main(String[] args) {
        // ${formatMessage('uns.mqttServer')}
        String broker = "tcp://${hostName}:${tcpPort}";
        // ${formatMessage('uns.mqttClientId')}
        String clientId = "JavaDemoClient2";
        // ${formatMessage('uns.mqttTopic')}
        String topic = "${multipleTopicPre}${topic}";
        // ${formatMessage('uns.mqttQos')}
        int qos = 1;

        try {
            // ${formatMessage('uns.mqttCreateClient')}
            MqttAsyncClient client = new MqttAsyncClient(broker, clientId);
            // ${formatMessage('uns.mqttSetOptions')}
            MqttConnectOptions options = new MqttConnectOptions();
            options.setCleanSession(true);
            options.setConnectionTimeout(10);
            options.setAutomaticReconnect(true);

            // ${formatMessage('uns.mqttConnect')}
            client.setCallback(new MqttCallback() {
                @Override
                public void connectionLost(Throwable cause) {
                    System.out.println("Connection to MQTT broker lost!");
                }

                @Override
                public void messageArrived(String topic, MqttMessage message) throws Exception {
                    System.out.printf("Message arrived. Topic: %s Message: %s%n", topic, new String(message.getPayload()));
                }

                @Override
                public void deliveryComplete(IMqttDeliveryToken token) {
                    System.out.println("Delivery is complete!");
                }
            });

            // ${formatMessage('uns.ConnectToMQTTServer')}
            System.out.println("Connecting to broker: " + broker);
            IMqttToken token = client.connect(options);
            token.waitForCompletion();
            if (token.isComplete() && token.getException() == null) {
                System.out.println("Connected with result code " + token.getResponse().toString());
            }

            // ${formatMessage('uns.mqttTopic')}
            System.out.println("Subscribing to topic: " + topic);
            client.subscribe(topic, qos);

            // ${formatMessage('uns.mqttDisconnect')}
            Thread.sleep(60000);
            client.disconnect();
            client.close();
        } catch (MqttException | InterruptedException e) {
            e.printStackTrace();
        }
    }
}
`;
  const jb = `
    <dependency>
      <groupId>org.eclipse.paho</groupId>
      <artifactId>org.eclipse.paho.client.mqttv3</artifactId>
      <version>1.2.5</version>
    </dependency>
    <dependency>
      <groupId>com.alibaba</groupId>
      <artifactId>fastjson</artifactId>
      <version>2.0.53</version>
    </dependency>`;

  return (
    <div className={styles['fetch-data-content']}>
      <div className={styles['fetch-data-info']}>
        <div className={styles['info-title']}>{formatMessage('uns.MQTTAccessPoint')}</div>
        <div className={styles['info-description']}>{formatMessage('uns.MQTTAccessMethod')}</div>
        <Tabs
          activeKey={tab}
          onChange={(t) => {
            setTab(t);
          }}
          items={[
            {
              label: 'JS',
              key: 'js',
              children: (
                <Flex gap={14} vertical>
                  <ComCopyContent label={formatMessage('uns.MQTTUrl')} textToCopy={mqttWebsocketUrl} />
                  <ComCopyContent label={formatMessage('uns.MQTTPort')} textToCopy={mqttWebsocketPort} />
                  <ComCopyContent label={formatMessage('uns.topic')} textToCopy={`${multipleTopicPre}${topic}`} />
                  <ComCopyContent label={formatMessage('uns.dependent')} textToCopy={'npm install mqtt'} />
                </Flex>
              ),
            },
            {
              label: 'JAVA',
              key: 'java',
              children: (
                <Flex gap={14} vertical>
                  <ComCopyContent label={formatMessage('uns.MQTTUrl')} textToCopy={`tcp://${hostName}`} />
                  <ComCopyContent label={formatMessage('uns.MQTTPort')} textToCopy={tcpPort} />
                  <ComCopyContent label={formatMessage('uns.topic')} textToCopy={`${multipleTopicPre}${topic}`} />
                  <ComCopyContent label={formatMessage('uns.dependent')} textToCopy={jb} />
                </Flex>
              ),
            },
          ]}
        ></Tabs>
      </div>
      <div className={styles['fetch-data-code']}>
        <ComCodeSnippet className="codeViewWrap" minCollapsedNumberOfRows={24} maxCollapsedNumberOfRows={24}>
          {tab === 'js' ? jscode : javacode}
        </ComCodeSnippet>
      </div>
    </div>
  );
};

const FetchData: FC<any> = ({ instanceInfo }) => {
  const formatMessage = useTranslate();
  return (
    <div className={styles['fetch-data']}>
      <Tabs
        className={styles['fetch-data-tab']}
        defaultActiveKey="1"
        items={[
          {
            label: formatMessage('uns.historyData'),
            key: '1',
            children: <HistoryData instanceInfo={instanceInfo} />,
          },
          {
            label: formatMessage('uns.realtimeData'),
            key: '2',
            children: <RealtimeData instanceInfo={instanceInfo} />,
          },
        ]}
      />
    </div>
  );
};

export default FetchData;
