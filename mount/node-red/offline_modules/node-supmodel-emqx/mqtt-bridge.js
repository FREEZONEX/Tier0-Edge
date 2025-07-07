
const q = require('./lib/queue');

class MqttBridge {

    queue;

    invalidMsg = "supmodel.error_mqtt_data_not_valid";

    timer;

    mappings;
    
    constructor(node, mappings, interval) {
        this.queue = q.newQueue();
        this.mappings = mappings;

        this.timer = setInterval(() => {
            let newMsg = this.queue.poll();
            if (newMsg != null) {
                node.send([newMsg, null])
            }
        }, interval); 

        console.log(new Date(), `: MQTT定时推送任务开启, 节点ID=${node.id}`)
    }

    /**
     * 输入数据和平台数据格式要保持一致
     * @param {} inputMsg 
     * {
     *   "values": [
     *   {
     *       "name": "string", // 位号名称
     *       "value": // json: RtdValue 位号实时值
     *       {
     *           "timeStamp": 1716894556000, 
     *           "quality": 0, // int, 质量码，0-标识正常值
     *           "value": 12, // 值，数字、字符串或布尔，输入的值与type对应
     *           "type": 1 /* int, 位号值类型，与type一致。1: int; 2: double; 3: bool; 4: string
     *       }
     *   }
     *   ]
     * }
     * @returns 
     */
    receive(msg, envs) {
        // if (this.invalidMsg) {
        //     this.invalidMsg = isValid(payload);
        // }
        // if (this.invalidMsg)  {
        //     return this.invalidMsg;
        // }
        if (this.queue) {
            let topicMap = transfer(msg, this.mappings, envs);
            for (let key in topicMap) {
                this.queue.offer({
                    topic: key,
                    payload: topicMap[key]
                });
            }
        }
    }

    destroy(nodeId) {
        clearInterval(this.timer);
        this.queue = null;
        this.mappings = null;
        console.log(new Date(), `: MQTT节点被销毁, 节点ID=${nodeId}`)
    }
}


function transfer(msg, mappings, envs) {
    
    let useAlias = envs.use_alias;

    let topicResult = {};

    if (!mappings) {
        topicResult[msg.topic] = msg.payload;
        return topicResult;
    }

    let unsArray = mappings[msg.topic];
    if (unsArray && unsArray.length > 0) {
        for (let i in unsArray) {
            if (useAlias === true) {
                topicResult[unsArray[i].alias] = msg.payload;
            } else {
                topicResult[unsArray[i].path] = msg.payload;
            }
        }
    } else {
        topicResult[msg.topic] = msg.payload;
    }
    return topicResult;
}

function newMqttBridge(node, mappings, interval) {
    return new MqttBridge(node, mappings, interval);
}

module.exports = { newMqttBridge }; // 导出函数