
const q = require('./lib/queue');

class MockDataBridge {

    queue;

    timer;

    constructor(node, interval) {
        this.queue = q.newQueue();

        this.timer = setInterval(() => {
            let newMsg = this.queue.poll();
            if (newMsg != null) {
                node.send([newMsg, null])
            }
        }, interval); // mock数据1秒推送一次

        node.log(`Mock Data定时推送任务开启, 节点ID=${node.id}`)
    }

    receive(inputMsg) {
        if (this.queue) {
            this.queue.offer(inputMsg);
        }
    }

    destroy(nodeId) {
        clearInterval(this.timer);
        this.queue = null;
        console.log(new Date(), `: Mock Data节点被销毁, 节点ID=${nodeId}`)
    }
}

function newMockDataBridge(node, interval) {
    return new MockDataBridge(node, interval);
}

module.exports = { newMockDataBridge }; // 导出函数