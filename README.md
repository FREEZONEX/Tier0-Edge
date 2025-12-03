# Tier0: An Open-Source IIoT Platform

[![Try Tier0](https://img.shields.io/badge/Try%20Tier0-Live%20Demo-blue?style=flat&logo=rocket&logoColor=red)](https://supos.ai/trial)
[![Docs](https://img.shields.io/badge/docs-available-brightgreen?style=flat&logo=readthedocs)](https://suposcommunity.vercel.app)
[![License](https://img.shields.io/badge/License-Apache_2.0-yellow?style=flat&logo=open-source-initiative)](./LICENSE)

**Tier0** is an open-source industrial data integration platform built on the **Unified Namespace (UNS)** methodology and powered by production-grade open-source technologies.

<img width="750" src="./assert/image/Update_open source program.jpg" />

---

## Architecture Overview
<img width="750" src="./assert/image/function_update.png" />

- **Source Flow**  
  Serves as the connection pipeline to devices and systems. It handles real-time protocol translation into JSON payloads. Built entirely on Node-RED.

- **Namespace**  
  The core of Tier0. A semantic MQTT broker and parser that models data using topic hierarchies and structured JSON payloads.

- **Sink**  
  The storage layer of Tier0.  
  - Time-series Namespace values are stored in **TimescaleDB**.  
  - Relational Namespace values (e.g., CRM data) are stored in **PostgreSQL**.  
  This enables efficient querying and compression.

- **Event Flow**  
  Orchestrates Namespaces into higher-level event/information flows. Supports merging JSON payloads and appending system-generated prompts for LLM-powered optimization.

---

## Hardware Requirements

|             | Minimum Requirement                  | Recommended Requirement                       |
|-------------|--------------------------------------|-----------------------------------------------|
| CPU         | 4 cores                              | 8 cores                                       |
| Memory      | 8 GB                                 | 16 GB                                         |
| Disk        | 100 GB, 1000 IOPS (30% random write)      | 1 TB, 2000 IOPS (30% random write)        |
| Browser     | Chrome 89, Edge 89, Firefox 89, Safari 15 | Chrome 89, Edge 89, Firefox 89, Safari 15 |

## Deployment
> For detailed guides and advanced examples, see the <a href="https://suposcommunity.vercel.app/">Tier0 Community Docs</a>.
### 1.Linux
#### 1.1 Operating Environment
- **Operating System**: Currently tested on Ubuntu Server 24.04 with Docker. We welcome feedback on other OS distributions.
- **Docker**: We assume you have Docker (with `docker compose` and `buildx`) installed. Our tested versions:
  - Docker Engine - Community: 27.4.0
  - Docker Buildx: v0.19.2
  - Docker Compose: v2.31.0
  - containerd: 1.7.24
    
#### 1.2 Installing Tier0
1. Clone the project.
   ```bash
   git clone <this repo>
   ```
2. Navigate to the `Tier0` directory and edit environment variables in the `.env` file.
   ```bash
   cd Tier0
   vi .env
   ```
   - Update `VOLUMES_PATH` (directory for storing project data).
   - Update `ENTRANCE_DOMAIN` (frontend entry domain/IP address).
   - Modify other variables as needed.
3. Install Tier0.
   ```bash
   bash bin/install.sh
   ```
### 2.Windows
#### 2.1 Operating Environment
- Install the latest version of **Docker Desktop** and **Git** on Windows 10 or Windows 11.
- It is recommended to perform all operations in **Git Bash** for better compatibility.
#### 2.2 Installing Tier0
1. Clone the project using **Git Bash**.
   ```bash
   git clone <this repo>
   ```
2. Navigate to the `Tier0` directory and edit environment variables in the `.env` file.
   ```bash
   cd Tier0
   vi .env
   ```
   - Update `OS_PLATFORM_TYPE` = windows
   - Update `VOLUMES_PATH` (directory for storing project data).
   - Update `ENTRANCE_DOMAIN`  (Do not use 127.0.0.1 or localhost, otherwise login and authentication functions **will NOT** work.)
   - Modify other variables as required by the system.
3. Install Tier0.
   ```bash
   bash bin/install.sh
   ```
### 3. Access the Platform
   1. Visit `http://<YOUR-DOMAIN>:<YOUR-PORT>` in your browser (based on ENTRANCE_DOMAIN and ENTRANCE_PORT in `.env`).
   2. Sign in to Tier0 with default account and password: `supos/supos`.
---

## Important Startup Operations
### 1. UNS Data Model Creation
#### 1.1 Building Models Manually
> `Factory/workshop/equipment/CNC` will be used as an example, in which `Factory`, `workshop` and `equipment` are paths and `CNC` is a topic.
1. Log in to Tier0, and then select **UNS** > **Namespace**.
2. Under **Topic**, click <img src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/5.png" /> to add a path (e.g. `factory`).

<img width="450" src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/34.png" />

3. Select **equipment**, and then click <img src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/7.png" /> to add a topic (e.g.`CNC`) under it.

<img width="450" src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/36.png" />

4. Enter the information of the topic, and then click **Save**.

#### 1.2 Importing Models
1. Log in to Tier0, and then select **UNS** > **Namespace**.
2. Click **Import** at the upper-right corner.

<img width={450} src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/69.png" />

3. Import JSON to create models.
- Directly enter JSON.

<img width={450} src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/69-1.png" />

- Click **Upload File** to download the template and enter the model content according to template rules.

<img width={450} src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/69-2.png" />


> You can manually add a path and topic, export it and use it as an example for import.

> <img width={450} src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/69-3.png" />


4. Save the template file, and then click **Import** on the **Namespace** page.
5. Select the template, and then click **Save**.

#### 1.3 Generating Models through JSON
1. Log in to Tier0, and then select **UNS** > **Namespace**.
2. Click <img src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/40.png" /> on the **Topic** page.

<img width={350} src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/40-1.png" />

3. Select the data type, and then write JSON text for the model according to the data tree structure.

```json
{
    "Factory1": {
        "workshop1": {
            "equipment1": {
                "CNC1": [
                    {
                        "actualRuntime1": 1380,
                        "plannedRuntime1": 1440
                    }
                ]
            }
        }
    }
}
```

<img width={450} src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/41.png" />

4. Click **Next**, and select the model on the left side.
:::tip
Click on each data level, you can change its information and add attributes on the right side.
:::

<img width={450} src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/42.png" />

5. Click **Save**.

### 2. Model Data Source Connection
> Connect real data to make models alive.
1. Log in to Tier0, go to **UNS** > **Namespace**, and under the **Topic** tab, select a file.
2. Scroll down to **Topology**, click the icon on **Source Flow** to redirect to the generated data flow.

<img width="650" src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/49.png" />

3. Change the data source of the generated flow.

<img width="650" src="http://communityimage2.oss-cn-hangzhou.aliyuncs.com/51.png" />

4. Deploy and trigger the flow.
---
## License
This project is licensed under the [Apache 2.0 License](./LICENSE).

## Support & Contact
- 📖 [Documentation](https://suposcommunity.vercel.app)  
- 🐞 [GitHub Issues](./issues)

## Contributors
We gratefully acknowledge the following individuals for their contributions to Tier0:

**Wenhao Yu**, **Liebo**, **Weipeng Dong**, **Kangxi**, **Lifang Sun**, **Minghe Zhuang**,  
**Wangji Xin**, **Fayue Zheng & Yue Yang**, **Yanqiu Liu**, **Dongdong An**, **Jianan Zhu**
