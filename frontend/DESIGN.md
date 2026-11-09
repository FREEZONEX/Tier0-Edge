# Tier0 Enterprise Frontend Design Guidelines

## 1. 文档定位

本文件是 Tier0 Enterprise 前端的 UI 规范入口，用于在开发或调整页面、弹窗、表单、表格、卡片、导航等界面前快速统一设计判断。

它负责回答 4 个问题：

- Tier0 Enterprise 的产品界面应该呈现什么气质
- UI 设计的事实来源在哪里
- 新页面和新组件必须遵守哪些视觉与交互规则
- 更细的 token、主题和共享组件应去哪里查

本文件不负责：

- 路由、API、提交门禁等工程规范
- 组件源码级 API 说明
- 逐像素的页面实现细节

以上内容以 `CLAUDE.md`、`src/theme/`、`src/components/` 和业务代码为准。

## 2. 设计目标

Tier0 Enterprise 的界面应当呈现以下特征：

- 精确、克制、偏产品工具，而不是营销落地页
- 高密度、可操作、偏 enterprise workspace
- 通过排版、边框、分层和节奏建立层级，而不是依赖重装饰
- 以中性色为主，以蓝色作为主题强调色，而不是满屏主色铺陈

默认视觉方向：

- 背景以白、浅灰、深灰为主
- 核心主操作使用蓝色主题色
- 高亮、选中、激活态优先使用主题蓝
- 支持 light / dark / chartreuse 三种主题变体
- 权限与角色使用独立的语义色系（紫、蓝、灰、橙）
- 避免大面积渐变、装饰性图形、厚重阴影和过强的拟物效果

## 3. 事实来源

做 UI 时，先看代码里的事实来源，不要在业务页面重新发明一套规则。

- 基础色板变量：`src/theme/theme.scss`（所有 `--ui-t-*` 变量）
- 应用级语义变量：`src/index.scss`（`:root` 和 `.dark` 中的 `--ui-*` 变量）
- Ant Design token 桥接：`src/theme/theme-token.ts`
- 共享组件：`src/components/`
- 全局样式入口：`src/index.scss`
- CodeMirror 主题：`src/theme/codemirror-theme.tsx`

优先使用语义变量和共享组件，不要优先写局部样式覆盖。

## 4. 设计原则

### 4.1 层级优先

界面层级优先通过以下方式建立：

- 标题与正文的字号、字重差异
- 背景层次变化
- 边框和分隔线
- 模块间距与布局节奏

不要依赖这些方式作为主要层级手段：

- 大面积艳色背景
- 装饰性插画
- 无意义的阴影堆叠
- 过多渐变和动画

### 4.2 高密度优先

Tier0 Enterprise 多数页面是工作台，不是展示页。默认采用高密度布局：

- 表单、筛选、表格、侧栏、弹窗优先紧凑排布
- 内容区域优先服务操作效率和信息读取
- 页面主体通常是 header + controls + content 的 workspace 结构

除登录、引导、创建向导等少数场景外，不要把内部产品页做成居中展示型 landing page。

### 4.3 语义优先

颜色、状态、组件都应表达明确语义：

- 蓝色系承担主题操作与交互强调
- 灰色系承担文字层级与背景分层
- 紫色系承担高权限等级标识
- 橙色系承担角色标识
- success / warning / error / info 使用既有状态 token
- 不要把主题蓝当作"通用成功色"到处滥用

## 5. 基础视觉规则

### 5.1 颜色

颜色使用顺序：

1. 优先使用 `var(--ui-*)` 语义变量
2. 其次使用 `var(--ui-t-*)` 基础色板变量
3. 最后才考虑极少量局部补充（需注释说明原因）

#### 基础色板（`--ui-t-*`）

| 色阶 | Gray      | Blue      | Chartreuse        |
| ---- | --------- | --------- | ----------------- |
| 100  | `#161616` | `#061833` | `#242f06`         |
| 90   | `#262626` | `#0f3c7f` | `#3b4f0a`         |
| 80   | `#393939` | `#134fa9` | `#59770f`         |
| 70   | `#525252` | `#1d77fe` | `rgb(148 197 24)` |
| 60   | `#6f6f6f` | `#438efe` | `#b2ed1d`         |
| 50   | `#8d8d8d` | `#68a4fe` | `#bff043`         |
| 40   | `#a8a8a8` | `#68a4fe` | `#ccf368`         |
| 30   | `#c6c6c6` | `#8ebbff` | `#d9f68e`         |
| 20   | `#e0e0e0` | `#bbd6ff` | `#e5f9b4`         |
| 10   | `#f4f4f4` | `#e8f1ff` | `#f0fbd2`         |

其他色组：Magenta（`#2a0a18` → `#fff0f7`）、Teal（`#081a1c` → `#d9fbfb`）

#### 应用级语义变量（Light 主题）

| 角色         | Token                               | 解析值             | 用途                 |
| ------------ | ----------------------------------- | ------------------ | -------------------- |
| 主文字       | `--ui-text-color`                | `#161616`          | 标题和主体文字       |
| 反色文字     | `--ui-anti-text-color`           | `#fff`             | 深色背景上的文字     |
| 页面背景     | `--ui-bg-color`                  | `#fff`             | 主画布               |
| 主题色       | `--ui-theme-color`               | `#1d77fe`          | 主按钮、链接、激活态 |
| 主题 hover   | `--ui-theme-button-hover-color`  | `#134fa9`          | 主按钮悬停           |
| 主题 active  | `--ui-theme-button-active-color` | `#0f3c7f`          | 主按钮按下           |
| 边框         | `--ui-border-color`              | `#393939`          | 标准分隔线           |
| 菜单 hover   | `--ui-menu-hover-color`          | `#e0e0e0`          | 菜单项悬停           |
| 菜单 active  | `--ui-menu-active-color`         | `#e6f4ff`          | 菜单项选中           |
| 卡片背景     | `--ui-card-color`                | `#fff`             | 卡片默认底色         |
| 卡片 hover   | `--ui-card-hover-color`          | `#e8f1ff`          | 卡片悬停             |
| 输入框背景   | `--ui-input-color`               | `#fff`             | 输入框底色           |
| 弹窗背景     | `--ui-modal-color`               | `#fff`             | 弹窗内容区           |
| 弹窗遮罩     | `--ui-modal-mask-bg`             | `rgb(0 0 0 / 45%)` | 弹窗背景遮罩         |
| 表格行 hover | `--ui-charttop-bg-color`         | `#f4f4f4`          | 表格行悬停           |
| 标签背景     | `--ui-tag-color`                 | `#f4f4f4`          | 标签默认底色         |
| 填充色       | `--ui-fill-secondary`            | `rgb(0 0 0 / 6%)`  | 次级填充             |
| 填充色弱     | `--ui-fill-tertiary`             | `rgb(0 0 0 / 4%)`  | 弱填充               |
| 错误色       | `--ui-error-color`               | `#da1e28`          | 错误状态             |

#### 权限等级色（Enterprise 专有）

| 等级              | 背景                                    | 边框                                        | 文字                                      |
| ----------------- | --------------------------------------- | ------------------------------------------- | ----------------------------------------- |
| Purple（高权限）  | `--ui-access-purple-bg`（`#faf5ff`） | `--ui-access-purple-border`（`#d8b4fe`） | `--ui-access-purple-text`（`#9333ea`） |
| Blue（标准权限）  | `--ui-access-blue-bg`（`#e8f1ff`）   | `--ui-access-blue-border`（`#8ebbff`）   | `--ui-access-blue-text`（`#1d77fe`）   |
| Gray（只读/受限） | `--ui-access-gray-bg`（`#f4f4f4`）   | `--ui-access-gray-border`（`#c6c6c6`）   | `--ui-access-gray-text`（`#6f6f6f`）   |

#### 角色色（Enterprise 专有）

| 角色               | 背景                                  | 边框                                      | 文字                                    |
| ------------------ | ------------------------------------- | ----------------------------------------- | --------------------------------------- |
| Orange（角色标识） | `--ui-role-orange-bg`（`#fff4e5`） | `--ui-role-orange-border`（`#ffb380`） | `--ui-role-orange-text`（`#ff832b`） |

#### 状态色

| 状态     | 背景                                    | 文字                                      | 边框                                        |
| -------- | --------------------------------------- | ----------------------------------------- | ------------------------------------------- |
| Active   | `--ui-status-active-bg`（`#f6fef9`） | `--ui-status-active-text`（`#53b483`） | `--ui-status-active-border`（`#b8f182`） |
| 黄色警告 | —                                       | `--ui-t-yellow-color`（`#f1c21b`）     | —                                           |
| 橙色提示 | —                                       | `--ui-t-orange-color`（`#ff832b`）     | —                                           |
| 错误     | —                                       | `--ui-error-color`（`#da1e28`）        | —                                           |

#### 主题变体

项目支持三种主题，通过 CSS class 切换：

- 默认（Light）：`:root` 中定义
- 暗色（Dark）：`.dark` class
- 黄绿（Chartreuse）：`.chartreuse` / `.chartreuseDark` class

Dark 模式下权限等级色会自动映射到对应深色变体（如紫色映射到 Magenta 深色系）。

禁止：

- 在业务页面硬编码十六进制颜色作为常规方案
- 引入不在主题体系中的"新主色"
- 不考虑 dark 模式兼容性直接写死颜色
- 权限/角色标识使用非预定义的颜色

### 5.2 排版

#### 字体

全局使用 IBM Plex Sans，通过 `@ibm/plex-sans` SCSS 模块引入：

| 用途         | 字体                                         | 加载方式              |
| ------------ | -------------------------------------------- | --------------------- |
| 全局 UI 字体 | IBM Plex Sans                                | `@ibm/plex-sans/scss` |
| 系统回退     | system-ui, -apple-system, BlinkMacSystemFont | fallback              |

完整 font-family 声明：

```
'IBM Plex Sans', system-ui, -apple-system, BlinkMacSystemFont, '.SFNSText-Regular', sans-serif
```

加载的字重：regular、light、semibold、italic（其余已禁用以减小体积）。

#### 字号原则

- 标题负责层级，正文负责可读性
- 产品页优先稳定、克制，不追求海报式排版
- 不自行引入新的展示字体
- Ant Design 组件内字号跟随其 token 配置，不额外覆盖

### 5.3 间距与圆角

默认遵循紧凑布局节奏。

推荐节奏：

- 常用间距：8px、12px、16px、20px、24px
- 常用面板内边距：12px、16px、20px
- 默认组件圆角：3px（Ant Design token `borderRadius: 3`）
- 大圆角仅在现有上下文已经使用时才跟随

原则：

- 同一页面尽量只使用少量稳定的间距档位
- 不要让卡片、弹窗、按钮各自使用不同的圆角语言

### 5.4 边框与阴影

Tier0 Enterprise 的分层主要依赖边框和背景层次，不依赖重阴影。

默认规则：

- 常规面板和表格使用中性细边框
- 弹层、下拉、弹窗使用 `--ui-boxshadow-color` 定义的轻量阴影
- 日常 dashboard 卡片避免大而软的阴影
- 强描边只用于 focus、selected、danger 或画布型场景

## 6. 组件级规范

### 6.1 按钮

优先使用 `src/components/com-button`（封装了 Ant Design Button）。

#### 类型（Ant Design Button type）

| Type      | 说明         | 主题色映射                         |
| --------- | ------------ | ---------------------------------- |
| `primary` | 主操作       | `--ui-theme-color`（`#1d77fe`） |
| `default` | 次级操作     | `--ui-bg-color` 底 + 边框       |
| `dashed`  | 虚线边框操作 | —                                  |
| `text`    | 纯文字操作   | —                                  |
| `link`    | 链接操作     | `--ui-theme-color`              |

#### 尺寸（Ant Design Button size）

| Size     | 说明     |
| -------- | -------- |
| `large`  | 大按钮   |
| `middle` | 默认     |
| `small`  | 紧凑按钮 |

#### ComButton 特性

- 支持 async onClick（自动管理 loading 状态）
- 集成权限校验（auth wrapper）
- 使用方式与 Ant Design Button 一致

规则：

- 一个局部决策区域通常只有一个主操作
- 蓝色主按钮用于核心确认
- 图标按钮保持克制，业务页面优先使用 `@carbon/icons-react`；现有 Ant Design 封装中的图标可沿用
- 按钮文案简短，且必须走 i18n

### 6.2 表单

表单应优先保证可扫读性和可维护性。

规则：

- 优先使用 Ant Design Form 体系
- 输入框背景使用 `--ui-input-color`
- 聚焦边框使用 `--ui-theme-color`
- 禁用态背景使用 `--ui-t-button-d-color`
- 字段密度保持紧凑，不做营销页式大间距表单
- 校验、占位、标签、帮助文案必须国际化
- 稍复杂表单应提取 `initialValues`、`rules`、字段配置和 options
- 不要在 JSX 中内联大段字段规则和复杂 label 节点

### 6.3 弹窗

优先使用 `src/components/pro-modal`（ProModal）。

#### ProModal 尺寸预设

| Size  | 宽度   |
| ----- | ------ |
| `xxs` | 400px  |
| `xs`  | 600px  |
| `sm`  | 800px  |
| `md`  | 1000px |
| `lg`  | 1200px |

#### ProModal 特性

- 可拖拽（`draggable`，默认开启）
- 可全屏（`fullScreenable`，默认开启）
- 居中显示（`centered`，默认开启）
- 控制按钮：全屏切换 + 关闭（32px × 32px）
- 弹窗背景：`--ui-modal-color`
- 遮罩：`--ui-modal-mask-bg`

规则：

- 弹窗默认作为中高密度操作容器
- 标题、内容、底部操作区层级明确
- 长内容使用稳定滚动区，不让整体布局跳动
- 除特别阻断式流程外，保留清晰的关闭路径
- 弹窗内所有文案必须走 i18n

### 6.4 表格与列表

表格和资源列表应体现"可操作工作台"的气质。

Ant Design Table token 配置：

- 表头背景：`--ui-bg-color`（与页面背景一致，不使用灰色表头底）
- 表头文字：`--ui-text-color`，14px，`font-weight: 400`
- 表头行高：47px（`padding: 12px 16px`）
- 行 hover：`--ui-charttop-bg-color`
- 边框色：`--ui-table-tr-color`（`#c6c6c6`）
- 表头圆角：0（无圆角）
- 列宽：默认支持拖拽调整（`ProTable` 全局开启 `resizeable` + `columnFit`）
- 行内操作图标：16×16px（全局统一）

规则：

- 表头清晰但不过分抢眼；禁止表头加粗（`font-weight: 600`）
- 行 hover、selected 态使用既有中性背景
- 长文本显式处理截断、换行或 tooltip
- 行操作保持低噪音，不要每列都做强视觉按钮

### 6.5 卡片、标签与状态

卡片用于承载资源、摘要、状态块，不用于堆砌装饰。

规则：

- 卡片默认使用 `--ui-card-color`（白底）
- 卡片 hover 使用 `--ui-card-hover-color`（蓝色浅底）
- 标签默认使用 `--ui-tag-color`（`#f4f4f4`）
- 权限等级标签使用对应的 `--ui-access-*` 色组
- 角色标签使用 `--ui-role-orange-*` 色组
- 状态颜色要有明确语义，不做"看起来好看"的临时配色

### 6.6 抽屉

使用 `src/components/com-drawer`（ComDrawer）。

- 背景色：`--ui-bg-color`
- 文字色：`--ui-text-color`
- 关闭按钮使用 Carbon icon

## 7. 页面结构规范

多数内部产品页应遵循以下基本结构：

- Header：标题、描述、返回、主操作、次操作
- Controls：搜索、筛选、分段、视图切换、批量操作
- Content：表格、列表、画布、编辑器或详情区

要求：

- loading / empty / error 状态应保持内容区结构稳定
- 使用明确滚动容器和稳定布局边界
- 不要因为局部状态切换导致整页高度和布局频繁跳变

## 8. 交互与动效

动效服务于状态表达，不服务于装饰。

规则：

- 常规过渡聚焦在颜色、透明度、背景、边框、阴影
- 过渡时长保持短促，避免拖沓
- loading、disabled、selected、hover、focus 必须有清晰反馈
- 不使用与业务状态无关的装饰性动画

可以少量使用渐变或动态效果的场景：

- AI 思考中
- 构建中
- 需要表达"流动过程"的明确产品状态

## 9. 国际化要求

所有用户可见文案必须国际化（使用 `react-intl`），包括但不限于：

- 按钮
- 标题与描述
- 表单标签、占位、校验信息
- 表格列名
- 空状态、错误提示、tooltip、toast
- 权限等级名称和角色名称

日志和临时调试信息除外，但提交前应清理无关调试输出。

## 10. Do / Don't

Do：

- 先复用 `src/theme/` 和 `src/components/`
- 优先使用 `var(--ui-*)` 语义变量
- 保持高密度、清晰层级、稳定结构
- 让选中、激活、禁用、危险态都有明确反馈
- 确保 light / dark 两种主题下颜色正确
- 权限/角色标识使用预定义的语义色组

Don't：

- 不要在 feature 目录里再造一套设计系统
- 不要把内部产品页做成营销风 landing page
- 不要滥用主题蓝色
- 不要依赖重阴影、大渐变、强装饰做层级
- 不要写死用户可见文案
- 不要把复杂表单配置全部内联在 JSX
- 不要引入现有图标体系之外的新图标库
- 不要硬编码颜色而不考虑 dark 模式
- 不要为权限/角色标识随意创建新颜色

## 11. 风格化约束

当需求没有明确视觉稿，或只给出低保真草图时，默认按以下风格化约束执行。

### 11.1 页面气质

- 默认是产品工作台，不是品牌展示页
- 信息组织优先于装饰表达
- 视觉重点来自结构、层级和状态，而不是大色块和视觉特效
- 新页面应优先"像现有 Tier0 Enterprise 页面"，而不是追求单页出彩

### 11.2 布局风格

- 内部页面默认使用 workspace 结构：`Header + Controls + Content`
- 主内容区优先左对齐、上对齐，不做大面积居中排布
- 控件与信息块采用紧凑排布，避免大片留白造成"空"和"散"

禁止：

- 在内部产品页使用 landing page 式 hero 区
- 在普通业务页加入与任务无关的装饰背景

### 11.3 色彩风格

- 默认基底始终是白（light）或深灰（dark）+ 中性边框
- 蓝色只用于主操作、链接、激活、选中
- 紫色只用于高权限等级标识
- 橙色只用于角色标识
- success / warning / error 必须走对应语义色

禁止：

- 把主题蓝扩大为页面主背景色
- 为单个功能模块定义新的品牌主色
- 不考虑 dark 模式直接写死浅色方案
- 混用权限色和状态色

### 11.4 组件形态

- 默认圆角 3px
- 默认使用细边框和轻量背景层次，不依赖重阴影
- 按钮、输入框、表格、标签优先使用现有共享组件形态

禁止：

- 普通卡片使用大而软的阴影作为主要分层方式
- 在 feature 内随意重新定义按钮、弹窗、表格的基础视觉语言

### 11.5 动效风格

- 动效只用于表达状态变化
- 常规过渡短、轻、可预期

禁止：

- 与业务无关的装饰性动画
- 因动效导致布局位移、内容跳动或操作目标不稳定

## 12. 开发前快速检查

开始 UI 开发前，至少确认以下事项：

- 是否已有可复用的共享组件（`src/components/`）
- 是否已有对应语义变量（`--ui-*`）
- 页面是否符合 workspace 型产品结构
- 用户可见文案是否已规划 i18n key
- 当前方案是否与现有页面密度和风格一致
- 颜色方案是否兼容 dark 模式
- 权限/角色相关 UI 是否使用了预定义色组

## 13. UI Review Checklist

提交 UI 改动前，至少按以下清单自查一次。

### 13.1 风格一致性

- 页面整体是否仍然像 Tier0 Enterprise 的 workspace
- 是否延续了现有页面的密度、边框语言、按钮语气和信息层级
- 是否出现了新的主视觉颜色、新的圆角语言或新的阴影风格

### 13.2 颜色与 Token

- 是否优先使用了 `var(--ui-*)` 语义变量
- 是否出现了硬编码 hex 颜色
- 是否兼容 dark 模式（`.dark` class 下颜色是否正确）
- selected、hover、focus、disabled、danger 是否有明确且一致的视觉反馈
- 权限等级和角色标识是否使用了正确的预定义色组

### 13.3 组件复用

- 是否优先复用了 `src/components/` 中的共享组件
- 是否在 feature 目录里重新做了按钮、弹窗、表格基础样式
- 图标是否来自现有图标体系，业务新增图标是否优先使用 `@carbon/icons-react`

### 13.4 布局与信息结构

- 页面是否遵循 `Header + Controls + Content` 的基本组织方式
- 内容区是否有稳定的 loading / empty / error 状态容器
- 长文本、长列表、窄屏场景是否处理了截断和溢出

### 13.5 文案与交互

- 所有用户可见文案是否都已国际化
- 按钮文案、空状态、错误提示是否直接、清晰、可操作
- 动效是否只服务于状态表达

## 14. 与其他文档的分工

- `CLAUDE.md`：工程规范、架构边界、流程门禁、仓库级硬约束
- `DESIGN.md`：UI 设计方向、视觉规则、组件风格与页面结构原则
- `src/theme/`：主题与 token 的代码事实来源
- `src/components/`：共享组件的代码事实来源
