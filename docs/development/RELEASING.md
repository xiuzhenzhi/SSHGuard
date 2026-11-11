# 构建与发布

## 工作流

| 工作流 | 用途 |
|---|---|
| CI | 按改动范围检查代码；勾选 `full_validation` 可运行完整安装矩阵 |
| 构建与发布 | 准备版本、完整验证并创建 Release |
| Documentation | 构建并发布文档站 |

## 从 Actions 发布

1. 打开 Actions →「构建与发布」→ **Run workflow**。
2. 选择分支，通常为 `main`。
3. 版本留空会自动递增补丁号，也可填写 `0.4.0` 或 `0.4.0-rc.1`。
4. 等待验证与发布完成，在 Releases 下载产物。

自动准备版本的 Git 提交署名使用 `xiuzhenzhi <xiuzhengzhi66@gmail.com>`；GitHub 推送操作账号仍由工作流凭据决定。

已有版本不能覆盖。工作流中断时使用 **Re-run failed jobs**，继续同一版本。

如果失败原因是工作流本身，修复后在 `main` 手动运行，版本留空，并把 `existing_tag` 填为尚未发布的标签（例如 `v0.4.1`）。这会使用更新后的工作流验证原标签代码，不修改标签；已经创建 Release 的版本不能使用此入口。

代理验证器跟随工作流提交，读取原标签的 Nginx 模板和候选程序，使用临时证书进行 HTTPS/WebSocket 检查。修正验证器不会替换发行包中的程序或部署配置。

若仅 HTTPS 代理验证失败，可以运行「HTTPS proxy recheck」，填入原发布运行 ID。它核对程序和部署输入未变化，复用原安装包，只执行 HTTPS/WebSocket 检查，不重复构建或安装矩阵。这个流程不会自动发布；发布前还需核对原运行的其余必需检查均已通过。

## 手动打标签

同步主控 `VERSION`、API 默认版本、安装示例和发布说明；仅 Agent 输入变化时更新 `AGENT_VERSION`、API 的 `AgentVersion`、Agent 默认版本和安装器，提交并等待 CI 通过后执行：

```sh
git tag -a v0.4.1 -m "SSHGuard 0.4.1"
git push origin v0.4.1
```

标签须与 `VERSION` 一致。包含连字符的版本会发布为预发布版。

## 本地构建

```sh
node scripts/build/build-release.mjs amd64
node scripts/build/build-release.mjs arm64 --reuse-web --reuse-agents
node scripts/checks/check-agent-artifacts.mjs
node scripts/build/package-release.mjs
```

输出目录为 `dist/release`。`--reuse-agents` 要求先构建同版本 amd64 包。

## 发布产物

主控 Release 自 v0.4.0 起只上传以下四个文件；GitHub 自动生成的两份源码归档另计。

| 文件 | 内容 |
|---|---|
| `sshguard-linux-amd64.tar.gz` / `arm64` | 主控、Web、管理脚本、部署模板及本地 Agent 下载文件 |
| `sshguard.sh` | 统一安装、更新与服务管理入口 |
| `SHA256SUMS` | 对应以上三个文件的校验清单 |

`deploy.sh`、`quick-start.sh`、`update.sh`、`uninstall.sh` 和版本信息保留在安装包内，不再单独上传。发布说明直接显示在 Release 页面。Agent 的四个平台程序、安装卸载脚本及逐文件校验继续在独立的 `agent-v*` Release 中提供，兼容现有安装器。CI 内部验证包可包含更多文件，不等于公开下载列表。

完整发布流程包含跨平台测试、数据库测试和五组原生安装升级。验证结果见 [验证记录](VALIDATION.md)。

## 独立版本与按组件验证

主控版本由 `VERSION` 管理，Agent 版本由 `AGENT_VERSION` 管理，两者允许不同。自动准备版本比较上次 Agent 发布标签与当前代码：Agent 代码、共享网络工具、Go 依赖、安装/卸载脚本或 Agent 构建逻辑发生变化才更新 Agent 版本。缺失历史时采取完整构建。纯主控、前端或文档变化不会推进 Agent 版本。

主控构建读取两个版本；安装链接、远程更新目标和「已是当前版本」判断使用 Agent 版本。未变化的 Agent 从对应 `agent-v*` Release 下载四个平台产物并验证校验和与架构，不重复交叉编译。下载或校验失败会中止发布。本地复用需要 Git 标签历史及 `gh`。Web Docker 镜像仅构建前端，Agent 官方安装命令直接访问其 GitHub Release。

| 变化范围 | 常规 CI |
|---|---|
| 文档 | 文档链接检查和文档站；不启动数据库或应用构建 |
| 前端 | 前端测试和构建；构建产物按源码缓存 |
| Agent | Agent 与共享网络包测试；不启动 MySQL/PostgreSQL |
| 主控 API、SQL、存储、迁移 | 主控测试和数据库兼容矩阵 |
| 通知、GeoIP | 主控相关测试；不单独启动外部数据库 |
| 共享依赖、工作流、无法识别的路径 | 保守执行全部检查 |

Release 按上个标签到当前提交判断变化，仍构建并校验主控发布包。安装部署矩阵只在主控、数据库或部署相关变化时执行。手动 CI 的 `full_validation` 保留完整验证能力。数据库结构继续使用已有迁移编号，不随主控版本制造空迁移。

文档站只监听内容、配置和文档构建依赖；单独变更 `VERSION` 不再触发文档部署。真实发布说明变化仍会正常重建文档。

## 按改动范围检查与构建

普通推送与发布均按变更的模块组合执行；例如只改 Agent 和前端，就只触发这两个模块的检查。纯文档修改只触发 Documentation 工作流，不触发应用 CI。

| 改动范围 | 检查与构建 |
|---|---|
| 前端 `web/` | 前端测试、依赖检查和生产构建 |
| 主控入口、接口、GeoIP、通知 | 主控 Linux / Windows 测试与构建；涉及数据库访问的接口同时跑数据库矩阵 |
| 存储层、数据库迁移、数据库维护与保留策略 | SQLite、PostgreSQL、MySQL 数据库测试；发布时重新打包包含迁移的主控 |
| Agent、Agent 安装与卸载脚本 | Agent Linux / Windows 测试、服务与安装器验证；发布时构建变化的 Agent |
| 主控部署脚本和模板 | 部署脚本检查；发布时验证相关安装更新路径 |
| 文档、文档配置和文档依赖 | 链接检查、文档构建、文档站部署 |
| 主控与 Agent 共用代码 | 同时检查实际使用它的主控和 Agent；两者都变化时执行互通验证 |
| Go 依赖 | 主控、Agent、数据库消费者检查，不自动触发前端和文档 |
| CI 工作流或无法识别的路径 | 保守执行完整检查，避免漏掉依赖 |

仅版本值变化不会扩大 CI 范围；同一文件还改了逻辑时不能跳过。改动判断会检查修改前后的文件，删除或缺失历史也不能导致静默复用。

连续推送会取消同一分支的旧 CI，新任务以该分支最近成功且属于当前提交历史的 CI 为比较起点，覆盖被取消任务中尚未验证的改动。无法读取成功记录时执行完整检查。正式发布使用独立队列，不受普通分支推送取消影响。

未改动的前端从源码完全一致的已发布版本复用，下载后验证 SHA-256，且不会把旧 Agent 下载文件复制到新包。未改动的 Agent 保持 `AGENT_VERSION`，复用对应版本产物。无法找到可验证的旧产物时发布会停止，需执行完整验证，不能直接发布未校验文件。

完整发行包仍需要组装文件并写入当前版本信息，主控二进制通过 Go 构建缓存完成版本链接；这一步不会额外触发未改动模块的 CI。数据库迁移内嵌于主控，迁移变化必须重新构建主控产物。

手动 CI 的 `full_validation` 可强制完整验证。检查汇总严格区分应成功和应跳过的任务，相关任务失败、取消或意外跳过时不能发布。

## 容器镜像

正式 Release 发布成功后自动调用 `Publish container images`，从该版本的公开安装包校验 SHA-256 并组装主控、前端双架构镜像，不重新编译应用和 Agent。版本标签保持不变；只有 GitHub 当前最新正式版本能更新 `latest`，补发旧版或预发布版不会覆盖它。

可在 Actions 的 `Publish container images` 中手动输入已发布标签（如 `v0.4.0`）补发或重试，无需创建新版本。工作流关联当前仓库，并仅使用短期 `GITHUB_TOKEN` 写入 GHCR，不需要保存个人访问令牌。首次发布后应确认两个容器包的可见性为 Public，确保用户可以匿名拉取。

### SQLite 安装问题定向复验

如果仅修复原生安装脚本，可在 Actions 的 `SQLite installation recheck` 中填写已有 CI 的运行编号。流程先确认主控、Agent、前端及构建输入没有变化，再校验并复用已有程序产物，只覆盖当前安装脚本和服务模板，执行 SQLite 的四种安装模式。它不发布版本，也不重复运行前端、Agent、MySQL 或 PostgreSQL 的独立检查；应用代码变化时会拒绝复用。

## v0.4 起的版本基线

主控 `VERSION`、Agent `AGENT_VERSION` 与文档 `DOCS_VERSION` 本次均为 0.4.0。因 Go 标准库安全更新，Agent 本次同步重新构建；以后各自按自己的版本递增，不互相跟号。没有 Agent 改动则继续复用已发布的 Agent。

文档版本由 `DOCS_VERSION` 独立管理并显示在文档站标题中。文档内容更新时按文档自身变化调整此文件；主控发布脚本不修改文档版本。主控发布说明更新不代表文档版本必须同步主控版本。推送文档或 `DOCS_VERSION` 到 main 后，由 Documentation 工作流验证并部署 GitHub Pages。
