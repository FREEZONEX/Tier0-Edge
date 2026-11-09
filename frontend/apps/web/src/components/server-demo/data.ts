import { getIntl } from '@/stores/i18n-store.ts';
import { resolveMqttWebsocketPort } from '@/utils/mqtt';
import type { TabItems } from './index';

interface ConfigTypes {
  title?: string;
  tabItems: TabItems[];
}

const data: { [x: string]: ConfigTypes | ((arg0: any) => ConfigTypes) } = {
  restApi: (params: any): ConfigTypes => {
    return {
      title: getIntl('restApiAccessMethod'),
      tabItems: [
        {
          label: 'curl',
          key: 'curl',
          leftFormItems: [
            {
              key: 'restApiUrl',
              label: getIntl('restApiUrl'),
              value: `${window.location.origin}/openapi/v1`,
            },
            {
              key: 'appSceretKey',
              label: getIntl('appSecretKey'),
              value: params.appSceretKey,
            },
            {
              key: 'instructions',
              label: getIntl('instructions'),
              type: 'html',
              value: `<strong>1. ${getIntl('createSecretKey')}</strong><br/><strong>2. ${getIntl('addSecretKeyTip')}</strong>`,
            },
          ],
        },
      ],
    };
  },
  websocket: () => ({
    title: getIntl('websocketAccessMethod'),
    tabItems: [
      {
        label: 'JS',
        key: 'js',
        leftFormItems: [
          {
            key: 'instructions',
            label: getIntl('instructions'),
            type: 'html',
            value: `
            <strong>1. ${getIntl('createSecretKey')}</strong>
            <br/>
            <strong>2. ${getIntl('addSecretKeyTip')}</strong>
            <br/>
            <strong>3. ${getIntl('commonHeaderTip')}</strong>
            <br/>
            <span style="white-space: pre-wrap">message json: { "head":{ "version":number//${getIntl('versionInfo')} " cmd":number//${getIntl('commandEnumerationValue')} }， "data":... }</span>
            <br/>version：1.0.0
            <br/>cmd：
            <table class="demo-table">
              <tr>
                <th>${getIntl('commandEnumerationValue')}</th>
                <th>${getIntl('messageDirection')}</th>
                <th>${getIntl('commandEnumerationType')}</th>
                <th>${getIntl('uns.description')}</th>
              </tr>
              <tr>
                <td>1</td>
                <td>${getIntl('clientToServer')}</td>
                <td>CMD_SUB</td>
                <td>${getIntl('realTimeDataSubscription')}</td>
              </tr>
              <tr>
                <td>2</td>
                <td>${getIntl('serverToClient')}</td>
                <td>CMD_SUB_RES</td>
                <td>${getIntl('subscriptionResponse')}</td>
              </tr>
              <tr>
                <td>3</td>
                <td>${getIntl('serverToClient')}</td>
                <td>CMD_VAL_PUSH</td>
                <td>${getIntl('realTimeValuePush')}</td>
              </tr>
              <tr>
                <td>4</td>
                <td>${getIntl('serverToClient')}</td>
                <td>CMD_META_PUSH</td>
                <td>${getIntl('metadataChangePush')}</td>
              </tr>
            </table>`,
          },
          {
            key: 'subscription',
            type: 'collapse',
            collapseItems: [
              {
                key: '1',
                label: getIntl('realTimeDataSubscription'),
                children: [
                  {
                    key: 'websocketUrl',
                    label: getIntl('websocketUrl'),
                    value: `ws://${window.location.host}/open-api/uns/ws`,
                  },
                  {
                    key: 'request',
                    label: getIntl('request'),
                    type: 'codeSnippet',
                    value: `{
    "head": {
        "cmd": 1,
        "version": string
    },
    "data": {
        "sub_real_value": {
            "${getIntl('fileAliasing')}A": {
                "all": true,   //${getIntl('allValues')}
                "part_value": []//${getIntl('partValues')}
            },
            "${getIntl('fileAliasing')}B": {
                "all": true,
                "part_value": []
            }
        }
    }
}`,
                  },
                  {
                    key: 'response',
                    label: getIntl('response'),
                    type: 'codeSnippet',
                    value: `{
    "head": {
        "cmd": 2,
        "version": string
    },
    "data": {
        "msg": "ok",
        "cmd": 1,
        "status": 200
    }
}`,
                  },
                  {
                    key: 'push',
                    label: getIntl('push'),
                    type: 'codeSnippet',
                    value: `{
    "head": {
        "cmd": 3,
        "version": string
    },
    "data": [
      {
        "alias": "string",
        "value":{"timeStamp":number, "status":number, "value":xxxxx}
      }
    ]
}`,
                  },
                ],
              },
              {
                key: '2',
                label: getIntl('eventSubscription'),
                children: [
                  {
                    key: 'websocketUrl',
                    label: getIntl('websocketUrl'),
                    value: `ws://${window.location.host}/open-api/uns/event/ws`,
                  },
                  {
                    key: 'request',
                    label: getIntl('request'),
                    type: 'codeSnippet',
                    value: `{
    "head": {
         "version": "1.0.0",
         "cmd": 5
     },
     "data": {
         "source": {
             "topic": [               // ${getIntl('supportWildcard')}
                "SIMEvent_Alarm_PADeviceConditionClassType_.*",
                "SIMEvent_Alarm_RotatingEquipmentConditionClassType_.*"
              ]
         }
     }
}
`,
                  },
                  {
                    key: 'response',
                    label: getIntl('response'),
                    type: 'codeSnippet',
                    value: `{
    "head": {
         "version": "1.0.0",
         "cmd": 2
     },
     "data": {
         "requestCmd": 5,            // ${getIntl('requestCommandNumber')}
         "msg": "",
         "code": number               // ${getIntl('responseCode')}
    }
}
`,
                  },
                  {
                    key: 'push',
                    label: getIntl('push'),
                    type: 'codeSnippet',
                    value: `{
    "head": {
         "version": "1.0.0",
         "cmd": 6
      },
     "data": {
         "source": "6a5375b29c63516de6c2a82e9cbb113c",  // ${getIntl('collectorAlias')}
         "payload": [
             {
                 "topic": "SIMEvent_Alarm_PADeviceConditionClassType_FAULT", // topic
                 "payload": []byte                    // ${getIntl('eventContent')}
             }
          ]
     }
}
`,
                  },
                ],
              },
            ],
          },
        ],
      },
    ],
  }),
  mqtt: (params: any): ConfigTypes => {
    const { dataType, hostName, topic, jsObj, tcpPort, fieldExampleList, mqttWebsocketUrl } = params;
    const mqttWebsocketPort = resolveMqttWebsocketPort(mqttWebsocketUrl);
    return {
      title: getIntl('mqttAccessMethod'),
      tabItems: [
        {
          label: 'JS',
          key: 'js',
          leftFormItems: [
            {
              key: 'MQTTUrl',
              label: getIntl('uns.MQTTUrl'),
              value: mqttWebsocketUrl,
            },
            {
              key: 'MQTTPort',
              label: getIntl('uns.MQTTPort'),
              value: mqttWebsocketPort,
            },
            {
              key: 'topic',
              label: getIntl('uns.topic'),
              value: topic,
            },
            {
              key: 'dependent',
              label: getIntl('uns.dependent'),
              value: 'npm install mqtt',
            },
            {
              key: 'payload',
              label: getIntl('uns.payload'),
              type: 'codeSnippet',
              isJSON: true,
              value: [1, 2, 3, 6].includes(dataType) ? jsObj : undefined,
            },
          ],
          rightFormItems: [
            {
              key: 'codeSnippet',
              type: 'codeSnippet',
              minCollapsedNumberOfRows: 26,
              maxCollapsedNumberOfRows: 26,
              value: `
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
        client.subscribe('${topic}', function (err) {
          console.log(err)
          if (!err) {
            client.publish('${topic}', JSON.stringify(${JSON.stringify(jsObj)}));
          }
        });
      });

      client.on('message', function (topic, message) {
        console.log(topic, message.toString());
      });
    `,
            },
          ],
        },
        {
          label: 'JAVA',
          key: 'java',
          leftFormItems: [
            {
              key: 'MQTTUrl',
              label: getIntl('uns.MQTTUrl'),
              value: `tcp://${hostName}`,
            },
            {
              key: 'MQTTPort',
              label: getIntl('uns.MQTTPort'),
              value: tcpPort,
            },
            {
              key: 'topic',
              label: getIntl('uns.topic'),
              value: topic,
            },
            {
              key: 'dependent',
              label: getIntl('uns.dependent'),
              value: `
    <dependency>
      <groupId>org.eclipse.paho</groupId>
      <artifactId>org.eclipse.paho.client.mqttv3</artifactId>
      <version>1.2.5</version>
    </dependency>
    <dependency>
      <groupId>com.alibaba</groupId>
      <artifactId>fastjson</artifactId>
      <version>2.0.53</version>
    </dependency>`,
            },
            {
              key: 'payload',
              label: getIntl('uns.payload'),
              type: 'codeSnippet',
              isJSON: true,
              value: [1, 2, 3, 6].includes(dataType) ? jsObj : undefined,
            },
          ],
          rightFormItems: [
            {
              key: 'codeSnippet',
              type: 'codeSnippet',
              minCollapsedNumberOfRows: 26,
              maxCollapsedNumberOfRows: 26,
              value: `
       import com.alibaba.fastjson.JSONObject;
       import org.eclipse.paho.client.mqttv3.*;

       public class MqttDemo {

        public static void main(String[] args) {
            //${getIntl('uns.mqttServer')}
            String broker = "tcp://${hostName}:${tcpPort}";
            //${getIntl('uns.mqttClientId')}
            String clientId = "JavaDemoClient1";
            //${getIntl('uns.mqttTopicPosted')}
            String topic = "${topic}";
            //${getIntl('uns.mqttQos')}
            int qos = 1;
            //${getIntl('uns.mqttMessage')}
            JSONObject root = new JSONObject();
            JSONObject source = new JSONObject();
            root.put("_source_", source);
            JSONObject resource = new JSONObject();
    ${fieldExampleList?.map((item: any) => `        resource.put("${item.key}", ${item.type === 'string' ? '"' + item.value + '"' : item.type === 'datetime' ? item.value + 'L' : item.value});`).join('\n')}
            String content = resource.toString();
            root.put("_resource_", resource);

            try {
                //${getIntl('uns.mqttCreateClient')}
                MqttAsyncClient client = new MqttAsyncClient(broker, clientId);
                //${getIntl('uns.mqttSetOptions')}
                MqttConnectOptions options = new MqttConnectOptions();
                options.setCleanSession(true);
                options.setConnectionTimeout(10);
                options.setAutomaticReconnect(true);

                //${getIntl('uns.mqttConnect')}
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

                //${getIntl('uns.ConnectToMQTTServer')}
                System.out.println("Connecting to broker: " + broker);
                IMqttToken token = client.connect(options);
                token.waitForCompletion();
                if (token.isComplete() && token.getException() == null) {
                    System.out.println("Connected with result code " + token.getResponse().toString());
                }

                //${getIntl('uns.mqttPublish')}
                for (int i = 0; i< 20; i++) {
                    MqttMessage message = new MqttMessage(content.getBytes());
                    message.setQos(qos);
                    System.out.println("Publishing message: " + content);
                    client.publish(topic, message);
                    Thread.sleep(1000);
                }


                //${getIntl('uns.mqttDisconnect')}
                Thread.sleep(10000);
                client.disconnect();
                client.close();
            } catch (MqttException | InterruptedException e) {
                e.printStackTrace();
            }
        }
    }`,
            },
          ],
        },
      ],
    };
  },
  dbInfo: () => ({
    title: getIntl('restConnect'),
    tabItems: [
      {
        label: 'JAVA',
        key: 'java',
        leftFormItems: [
          {
            key: 'url',
            label: getIntl('common.url'),
            value: 'jdbc:TAOS-RS://localhost',
          },
          {
            key: 'port',
            label: getIntl('common.port'),
            value: '6041',
          },
        ],
        rightFormItems: [
          {
            key: 'codeSnippet',
            type: 'codeSnippet',
            minCollapsedNumberOfRows: 26,
            maxCollapsedNumberOfRows: 26,
            value: `
  public static void main(String[] args) throws Exception {
      String jdbcUrl = "jdbc:TAOS-RS://localhost:6041?user=root&password=taosdata";
      try (Connection conn = DriverManager.getConnection(jdbcUrl)) {
          System.out.println("Connected to " + jdbcUrl + " successfully.");

          // you can use the connection for execute SQL here

      } catch (Exception ex) {
          // please refer to the JDBC specifications for detailed exceptions info
          System.out.printf("Failed to connect to %s, %sErrMessage: %s%n",
                  jdbcUrl,
                  ex instanceof SQLException ? "ErrCode: " + ((SQLException) ex).getErrorCode() + ", " : "",
                  ex.getMessage());
          // Print stack trace for context in examples. Use logging in production.
          ex.printStackTrace();
          throw ex;
      }
  }
`,
          },
        ],
      },
      {
        label: 'Python',
        key: 'python',
        leftFormItems: [
          {
            key: 'url',
            label: getIntl('common.url'),
            value: 'http://localhost',
          },
          {
            key: 'port',
            label: getIntl('common.port'),
            value: '6041',
          },
        ],
        rightFormItems: [
          {
            key: 'codeSnippet',
            type: 'codeSnippet',
            minCollapsedNumberOfRows: 26,
            maxCollapsedNumberOfRows: 26,
            value: `
  import taosrest

  def create_connection():
      conn = None
      url="http://localhost:6041"
      try:
          conn = taosrest.connect(url=url,
                                  user="root",
                                  password="taosdata",
                                  timeout=30)

          print(f"Connected to {url} successfully.");
      except Exception as err:
          print(f"Failed to connect to {url} , ErrMessage:{err}")
      finally:
          if conn:
              conn.close()
`,
          },
        ],
      },
      {
        label: 'Go',
        key: 'go',
        leftFormItems: [
          {
            key: 'taosDSN',
            label: 'taosDSN',
            value: 'root:taosdata@http(localhost:6041)/',
          },
          {
            key: 'port',
            label: getIntl('common.port'),
            value: '6041',
          },
        ],
        rightFormItems: [
          {
            key: 'codeSnippet',
            type: 'codeSnippet',
            minCollapsedNumberOfRows: 26,
            maxCollapsedNumberOfRows: 26,
            value: `
  package main

  import (
      "database/sql"
      "fmt"
      "log"
      _ "github.com/taosdata/driver-go/v3/taosRestful"
  )

  func main() {
      // use
      // var taosDSN = "root:taosdata@http(localhost:6041)/dbName"
      // if you want to connect a specified database named "dbName".
      var taosDSN = "root:taosdata@http(localhost:6041)/"
      taos, err := sql.Open("taosRestful", taosDSN)
      if err != nil {
        log.Fatalln("Failed to connect to " + taosDSN + "; ErrMessage: " + err.Error())
      }
      fmt.Println("Connected to " + taosDSN + " successfully.")
      defer taos.Close()
  }
`,
          },
        ],
      },
    ],
  }),
  mcpServer: () => ({
    title: getIntl('mcpServerAccessMethod'),
    tabItems: [
      {
        label: 'usage',
        key: 'usage',
        leftFormItems: [
          {
            key: 'install',
            label: getIntl('installation'),
            value: 'npm install -g @sup-platform/mcp-server',
          },
          {
            key: 'commandParameters',
            label: getIntl('commandLineParameters'),
            type: 'html',
            value: `
              <strong>${getIntl('commandLineParametersDesc')}:</strong>
              <br />
              <table class="demo-table">
                <tr>
                  <th>${getIntl('parameter')}</th>
                  <th>${getIntl('common.description')}</th>
                  <th>${getIntl('default')}</th>
                  <th>${getIntl('example')}</th>
                </tr>
                <tr>
                  <td>--port</td>
                  <td>${getIntl('portDesc')}</td>
                  <td>3000</td>
                  <td>--port 3000</td>
                </tr>
                <tr>
                  <td>--transport</td>
                  <td>${getIntl('transportDesc')}</td>
                  <td>stdio</td>
                  <td>--transport streamable</td>
                </tr>
                <tr>
                  <td>--ui-api-url</td>
                  <td>${getIntl('platformApiUrlDesc')}</td>
                  <td>-</td>
                  <td>--ui-api-url https://api.example.com</td>
                </tr>
                <tr>
                  <td>--ui-api-key</td>
                  <td>${getIntl('platformApiKeyDesc')}</td>
                  <td>-</td>
                  <td>--ui-api-key your-api-key</td>
                </tr>
                <tr>
                  <td>--openapi-path</td>
                  <td>${getIntl('openapiPathDesc')}</td>
                  <td>{api-base-url}/openapi/v1/swagger.json</td>
                  <td>--openapi-path http://api.example.com/openapi.yaml</td>
                </tr>
              </table>
            `,
          },
          {
            key: 'environmentVariables',
            label: getIntl('environmentVariables'),
            type: 'html',
            value: `
              <strong>${getIntl('environmentVariablesDesc')}:</strong>
              <br />
              <table class="demo-table">
                <tr>
                  <th>${getIntl('environmentVariables')}</th>
                  <th>${getIntl('common.description')}</th>
                  <th>${getIntl('correspondingCommandLineParameter')}</th>
                </tr>
                <tr>
                  <td>APP_API_URL</td>
                  <td>${getIntl('platformApiUrlDesc')}</td>
                  <td>--ui-api-url</td>
                </tr>
                <tr>
                  <td>APP_API_KEY</td>
                  <td>${getIntl('platformApiKeyDesc')}</td>
                  <td>--ui-api-key</td>
                </tr>
                <tr>
                  <td>APP_OPEN_PATH</td>
                  <td>${getIntl('openapiPathDesc')}</td>
                  <td>--openapi-path</td>
                </tr>
              </table>
            `,
          },
        ],
        rightFormItems: [
          {
            key: 'desktopApplicationIntegration',
            type: 'codeSnippet',
            label: getIntl('desktopApplicationIntegration'),
            subTitle: `${getIntl('desktopApplicationIntegrationDesc')}:`,
            minCollapsedNumberOfRows: 27,
            maxCollapsedNumberOfRows: 27,
            style: {
              backgroundColor: 'var(--ui-switchwrap-active-bg-color)',
            },
            value: `{
  "mcpServers": {
    "mcp-server-app-stdio": { // ${getIntl('stdioTransport')}
      "disabled": false,
      "timeout": 60,
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@sup-platform/mcp-server"],
      "env": {
        "APP_API_URL": "xxx",
        "APP_API_KEY": "xxx",
        "APP_OPEN_PATH": "xxx"
      }
    },
    "mcp-server-app-streamable": { // ${getIntl('streamableTransport')}
      "timeout": 60,
      "url": "http://localhost:3000/mcp", // ${getIntl('streamableServerUrl')}
      "type": "streamableHttp"
    }
  }
}`,
          },
        ],
      },
    ],
  }),
};

export default data;
