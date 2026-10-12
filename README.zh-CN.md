# Nagi — 适用于 macOS 和 Linux 的代理客户端

[English](README.md)

Nagi 是一款适用于 macOS 和 Linux 的代理客户端，提供原生 macOS 菜单栏应用、交互式终端菜单和命令行界面（CLI）。你可以用它切换代理节点、管理订阅和配置文件、设置路由规则与 DNS，以及控制 TUN 接入。

日常操作、实时流量速率和当前活动连接可通过 macOS 菜单栏应用查看和控制。高级配置、自动化、诊断与故障恢复可在两个平台上通过 CLI 完成。

[安装 Nagi](#安装) · [快速开始](#快速开始) · [用户指南](#用户指南) · [CLI 参考文档](docs/reference/cli.md)

## 功能

- **macOS 菜单栏应用**：查看引擎状态、选择代理、控制系统代理和 TUN、管理配置文件及订阅，并查看实时上传和下载速率；独立的 Connections 窗口支持搜索、排序、详情和断开连接。
- **终端菜单和 CLI**：提供分步操作、供脚本和应用使用的 JSON 输出，以及 Bash、Zsh 和 Fish 命令补全。
- **代理选择**：搜索节点、比较延迟、保存代理组选择，以及添加或刷新代理提供者。
- **订阅管理**：缓存更新、预览节点变化、定时刷新，并可选用 age 加密订阅源。
- **配置文件**：导入、切换、导出、备份、恢复和比较配置文件；本地覆盖配置与源配置分开应用。
- **路由和 DNS**：自定义规则、导入规则集、规则／全局／直连模式、DNS-over-HTTPS（DoH）策略，以及针对特定域名的 DNS 例外设置。
- **流量接入**：macOS 系统代理设置、macOS 和 Linux 上的 TUN 控制，以及代理监听端口和局域网访问设置。
- **监控与恢复**：查看和关闭连接、实时日志、只读诊断，以及登录时启动的服务。

CLI 支持 macOS 和 Linux 的 AMD64 与 ARM64 架构。可选的菜单栏应用要求 macOS 13 或更新版本。平台权限和路由行为详见[流量接入指南](docs/guides/traffic-access.md)。

## 安装

从 [GitHub Releases](https://github.com/ronigooja/Nagi/releases) 安装 CLI：

```sh
curl -fsSL https://raw.githubusercontent.com/ronigooja/Nagi/main/scripts/install.sh | bash
```

在 macOS 上，可通过 CLI 安装可选的菜单栏应用：

```sh
nagi app install
```

使用 `nagi update` 更新 CLI、mihomo 和已安装的应用；使用 `nagi app uninstall` 仅移除应用；使用 `nagi uninstall` 移除 Nagi 并保留用户数据。

下载的发布版本会根据归档文件的校验和进行验证。macOS 应用未签名，也未经过公证；首次启动时，可能需要在 Finder 的右键菜单中选择“打开”。权限与更新行为详见[安装和更新说明](docs/development/install.md#installed-cli-maintenance)。

## 快速开始

安装后，启动引擎、检查状态并打开终端菜单：

```sh
nagi start
nagi status
nagi menu
```

初始的 `default` 配置文件没有代理节点，流量会直连。要使用代理节点，请[导入自己的配置或应用订阅](docs/guides/getting-started.md)。启动引擎不会改变操作系统的系统代理设置；要启用 macOS 系统代理或 TUN 接入，请参阅[流量接入指南](docs/guides/traffic-access.md)。TUN 需要操作系统权限。

个人配置文件和订阅可使用 `work` 等名称；安装和更新会刷新 `default` 配置文件模板。详见[默认配置文件的行为](docs/development/install.md#install-update-and-remove)。

`nagi menu` 需要终端输入和输出。使用 `nagi --help` 查看命令，或使用 `nagi --json status` 获取结构化输出。交互细节详见[终端菜单参考文档](docs/reference/cli.md#terminal-menu)。

## 从源码构建

要求：Go 1.20 或更新版本，以及 Git。在仓库根目录构建 CLI 和与其版本锁定的代理引擎：

```sh
./scripts/build.sh
./bin/nagi --json version
```

如果已有匹配的 `bin/mihomo`，`make build cli` 只会构建 CLI。在 macOS 上，`make build app` 可单独构建菜单栏应用。关于锁定的源码检出版本，请参阅[构建与测试](docs/development/build-and-test.md)；关于安装本地构建产物，请参阅[安装](docs/development/install.md#install-update-and-remove)。

Nagi 使用 [mihomo](https://github.com/MetaCubeX/mihomo) 实现代理协议、DNS 解析、路由和流量转发。Nagi 通过私有 Unix Socket 控制 API 管理配置和进程生命周期。匹配的源码提交记录在 [`engine.lock`](engine.lock) 中；`NAGI_MIHOMO_BIN` 可指定运行时使用的可执行文件。

## 文档

### 用户指南

- [入门指南](docs/guides/getting-started.md)：初始配置与操作。
- [macOS 菜单栏应用](macos/NagiApp/README.md)：菜单控制、流量显示、安装与打包。
- [流量接入](docs/guides/traffic-access.md)：系统代理、TUN、监听端口和局域网访问。
- [代理提供者](docs/guides/proxy-providers.md)：添加节点来源、将其关联到代理组并刷新节点。
- [规则](docs/guides/rules.md)：生效的规则、自定义规则和规则集。
- [启动与恢复](docs/guides/startup.md)：登录服务、残留状态和重新检查。
- [诊断](docs/guides/diagnostics.md)：经过脱敏处理的健康和安全检查。

### 参考文档与设计

- [CLI 参考文档](docs/reference/cli.md)：命令、输出和退出码。
- [DNS 配置](docs/reference/cli.md#dns-policy-and-checks)：DoH 策略、域名例外设置和配置检查。
- [macOS 特权 TUN 辅助程序](docs/reference/cli.md#traffic-access)：TUN 接入的安装和后端选择。
- [架构](ARCHITECTURE.md)：系统边界和模块关系。
- [macOS 连接窗口设计](docs/design/connections-window.md)：快照调度、取消和操作协调。
- [终端菜单设计](docs/design/terminal-menu.md)：当前菜单的职责、导航、命令调用和故障处理。
- [运行时设计](docs/design/runtime.md)：进程、Socket、配置和恢复行为。
- [架构决策](docs/decisions/0001-pin-mihomo-commit.md)：mihomo 源码版本锁定。
- [受限 root 配置决策](docs/decisions/0003-restricted-root-configuration.md)：特权辅助程序与 mihomo 的验证边界。
- [macOS TUN DNS 决策](docs/decisions/0004-macos-tun-dns.md)：系统 DNS 的管理、恢复和代理目标地址解析。

### 开发与发布

- [构建与测试](docs/development/build-and-test.md)：锁定版本的构建和测试。
- [安装与更新](docs/development/install.md)：GitHub Release 安装、本地构建产物、更新和移除。
- [CLI 易用性规则](docs/development/cli-usability.md)：命令帮助、人类可读输出、可操作的错误信息和兼容性检查。
- [发布流程](docs/development/release.md)：版本规则、发布检查和发布说明要求。
- [Linux 特权辅助程序脚手架](docs/development/privileged-linux.md)：系统服务边界和当前可用状态。
- [功能路线图](docs/development/roadmap.md)：计划中的用户功能。
