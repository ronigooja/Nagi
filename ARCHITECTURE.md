# Nagi 架构实施方案

## 1. 目标与边界

Nagi 是以 CLI 为核心的 mihomo 管理工具：

- CLI 支持 macOS 和 Linux，并提供完整功能。
- SwiftUI 仅支持 macOS，只负责常用操作和状态展示。
- mihomo 使用 https://github.com/MetaCubeX/mihomo.git 的 Meta 分支作为上游。
- mihomo 的定制修改维护在 /root/mihomo，不复制到 Nagi 仓库。
- SwiftUI 不直接访问 mihomo API、配置文件或 Unix Socket，统一调用 Nagi CLI。

系统关系：

    SwiftUI macOS
          │ Process + JSON
          ▼
    Nagi CLI（macOS / Linux）
          │
          ├── mihomo 进程生命周期管理
          ├── mihomo REST API 客户端
          ├── 配置、Profile、订阅和状态管理
          └── launchd / systemd 集成
                │ Unix Domain Socket
                ▼
          mihomo Meta（定制分支）

Nagi 不重新实现代理核心能力。代理协议、DNS、规则、连接处理等功能仍由 mihomo 提供；Nagi 负责启动、配置、订阅、节点选择和用户界面。

## 2. 仓库边界

### /root/mihomo

mihomo fork 负责：

- 跟踪官方 Meta 分支；
- 保存 mihomo 核心定制；
- 编译 mihomo 可执行文件；
- 记录定制行为和 mihomo 专用配置。

建议分支：

    upstream/Meta       官方上游分支
    nagi/meta           Nagi 使用的定制分支
    fork                自己的远程仓库

CLI、SwiftUI、launchd、systemd 逻辑不加入 mihomo。只有必须修改 mihomo 核心行为的内容才进入该仓库。

### /root/Nagi

Nagi 产品仓库负责：

- Nagi CLI；
- mihomo 进程管理；
- mihomo REST API 调用；
- Profile、订阅和节点管理；
- macOS SwiftUI 应用；
- launchd、systemd 和发行包；
- 产品级测试和发布流程。

Nagi 通过 engine.lock 固定 mihomo 的仓库、分支和 commit。

## 3. 推荐目录

    Nagi/
    ├── cmd/nagi/main.go             # 程序入口
    ├── internal/
    │   ├── app/                     # 依赖组装、启动和关闭
    │   ├── command/                 # CLI 命令、参数解析和输出
    │   ├── control/                 # REST API / Unix Socket 客户端
    │   ├── engine/                  # 进程、PID、日志和退出状态
    │   ├── profile/                 # Profile 与 YAML 配置
    │   ├── subscription/            # 订阅增删改、刷新和缓存
    │   ├── proxy/                   # 代理组、节点、连接和选择状态
    │   ├── runtime/                 # socket、PID、锁和运行目录
    │   ├── output/                  # 人类可读和 JSON 输出
    │   ├── schema/                  # CLI JSON schema
    │   └── platform/                # macOS / Linux 差异
    ├── macos/NagiApp/               # SwiftUI 工程
    ├── packaging/launchd/
    ├── packaging/systemd/
    ├── scripts/
    ├── docs/
    ├── engine.lock
    ├── Makefile
    └── go.mod

command 只负责参数、业务调用和输出；业务逻辑放在 profile、subscription、proxy、engine 等功能包中。control 和 engine 必须分开：前者调用已运行的 mihomo，后者负责启动、停止和监控进程。

## 4. mihomo 上游与定制同步

在 /root/mihomo 中建立上游远程：

    cd /root/mihomo
    git remote rename origin fork
    git remote add upstream https://github.com/MetaCubeX/mihomo.git
    git fetch upstream Meta

定制分支基于上游 Meta：

    git switch -c nagi/meta upstream/Meta

如果当前分支已经包含定制提交，应保留这些提交并整理为 nagi/meta。每项定制保持独立提交，例如：

    fix: preserve proxy destination hostname
    feat: add preserve-proxy-hostname option

同步上游：

    git fetch upstream Meta
    git switch nagi/meta
    git rebase upstream/Meta

定制分为三类：

1. 可以普遍改善 mihomo 的修复，优先提交到上游；
2. Nagi 必需但不适合上游的 mihomo 行为，保留在 nagi/meta；
3. CLI、SwiftUI、订阅菜单、服务管理和发布逻辑，放在 Nagi。

每次修改 mihomo 的定制行为，都更新 mihomo 仓库中的 docs/customizations.md。Nagi 不重复维护 mihomo 定制说明。

## 5. 版本锁定与构建

根目录 engine.lock 记录精确依赖：

    repository: https://github.com/your-org/mihomo.git
    ref: nagi/meta
    commit: 0000000000000000000000000000000000000000

开发时允许使用本地 checkout：

    MIHOMO_DIR=/root/mihomo make build

CI 和发布构建按以下顺序查找 mihomo：

1. MIHOMO_DIR；
2. 按 engine.lock 下载并 checkout 精确 commit；
3. 无法得到精确 commit 时失败。

nagi version 应显示 Nagi 版本、mihomo 版本、mihomo commit、操作系统和架构。

## 6. CLI 设计

CLI 是完整功能入口：

    nagi start
    nagi stop
    nagi restart
    nagi status
    nagi logs

    nagi config validate
    nagi config show

    nagi profile list
    nagi profile use <name>

    nagi subscription list
    nagi subscription add <name> <url>
    nagi subscription update <name>
    nagi subscription remove <name>

    nagi proxy groups
    nagi proxy show <group>
    nagi proxy select <group> <node>
    nagi connections list

    nagi service install
    nagi service uninstall
    nagi version

每个命令支持人类可读输出和机器输出：

    nagi status
    nagi --json status
    nagi --json proxy groups

JSON 输出是 SwiftUI 的稳定接口。成功响应示例：

    {
      "ok": true,
      "data": {
        "running": true,
        "version": "mihomo-meta",
        "mixed_port": 17890
      }
    }

错误响应使用非零退出码，并输出稳定的 code 和 message。SwiftUI 只依赖 JSON，不解析表格、日志文字或 YAML。

## 7. mihomo 进程和控制接口

Nagi 启动 mihomo 时优先使用 Unix Domain Socket：

    macOS: ~/Library/Application Support/Nagi/runtime/mihomo.sock
    Linux: $XDG_RUNTIME_DIR/nagi/mihomo.sock

默认不开放 TCP 控制端口。用户明确需要远程管理时，才允许配置 external-controller。

engine 负责创建运行目录、校验配置、启动子进程、保存 PID、收集日志、检查存活、停止重启和避免重复启动。control 负责 version、configs、proxies、connections、订阅刷新、配置重载、超时和错误转换。

第一版不实现无限自动重启。异常退出后显示明确状态和最近日志，避免配置错误导致重启循环。

## 8. 配置、Profile 与运行数据

配置、缓存和运行状态分离：

    Application Support/Nagi/
    ├── config/
    │   ├── profiles/default.yaml
    │   ├── subscriptions.yaml
    │   └── settings.yaml
    ├── runtime/
    │   ├── mihomo.sock
    │   ├── mihomo.pid
    │   └── logs/
    ├── cache/subscriptions/
    └── state/selected-nodes.json

Linux 使用 XDG_CONFIG_HOME、XDG_DATA_HOME、XDG_STATE_HOME 和 XDG_RUNTIME_DIR 下的 nagi 目录。macOS 使用 ~/Library/Application Support/Nagi。

所有写配置操作：

1. 读取并校验旧配置；
2. 写入临时文件；
3. 原子替换目标文件；
4. 保留最近一次备份；
5. 请求 mihomo 校验或重载。

SwiftUI 不直接写配置文件。

## 9. SwiftUI 范围

SwiftUI 只提供：

- 当前运行状态；
- 启动、停止、重启；
- 当前 Profile；
- 代理组和节点切换；
- 订阅刷新；
- 简单连接列表；
- 最近日志；
- 基础设置。

完整 YAML 编辑、规则集转换、高级 DNS、批量订阅、调试诊断和脚本化操作保留在 CLI。

SwiftUI 通过 Process 调用：

    nagi --json status
    nagi --json proxy groups
    nagi --json subscription update <name>

第一版使用短命令轮询状态。需要实时更新时增加 nagi events --json，由 CLI 通过 stdout 输出事件。

## 10. 后台服务

macOS 使用用户级 launchd Agent；Linux 使用 systemd --user Service。服务文件由 CLI 生成：

    nagi service install
    nagi service uninstall

SwiftUI 不直接操作 launchd 或 systemd，只调用 nagi start、nagi stop、nagi status。

## 11. 安全要求

- 默认只使用 Unix Socket，不监听公网地址；
- Socket 文件权限限制为当前用户；
- 控制接口 Secret 不写入日志；
- 订阅 URL 不输出到普通日志；
- 配置写入使用临时文件和原子替换；
- 校验 Profile、路径和外部命令参数；
- SwiftUI 只执行固定的 Nagi CLI 路径，不拼接未经校验的 shell 命令。

## 12. 测试边界

Nagi 单元测试覆盖路径解析、engine 状态、Profile 原子写入、订阅合并、JSON schema、错误码和 CLI 参数。

mihomo 集成测试覆盖最小配置启动、Unix Socket、version/configs/proxies、节点切换、配置重载和异常退出。

SwiftUI 验证 CLI 不存在、mihomo 未运行、JSON schema 兼容、启停、节点切换和订阅刷新。

## 13. 实施阶段

### 阶段一：CLI 骨架

初始化 Go module；实现路径和运行目录；加入 engine.lock；实现 version、status、start、stop；编译并启动 /root/mihomo。

### 阶段二：mihomo 控制层

实现 Unix Socket HTTP Client；接入 version、configs、proxies、connections；统一错误码和 JSON 输出；增加配置校验和安全写入。

### 阶段三：完整 CLI

实现 Profile、订阅、代理组、节点、连接、日志、诊断和 launchd/systemd 服务安装。

### 阶段四：SwiftUI

创建 macOS App；实现 NagiCLIClient；实现 Dashboard、Proxies、Subscriptions、Settings；增加状态轮询和错误处理。

### 阶段五：发布

构建 macOS universal binary、Linux amd64/arm64；关联 mihomo 版本；完成 macOS 签名、公证、服务文件打包和 CI 锁定构建。

## 14. 长期约束

1. CLI 是完整功能的唯一入口；
2. SwiftUI 只能调用 CLI，不复制业务逻辑；
3. Nagi 不直接修改 mihomo 源码；
4. mihomo 核心定制只存在于 /root/mihomo；
5. Nagi 不依赖未锁定的 mihomo commit；
6. 默认使用 Unix Socket，不默认开放 TCP 控制端口；
7. 命令行和图形界面使用同一套业务实现。
