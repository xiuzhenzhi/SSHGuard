# RDP、VNC 与控制台登录

先更新主控，再更新 Agent。主控启动时执行 `012_remote_logins.sql` 迁移，保留原有 SSH 历史；旧主控会拒绝新事件类型。事件列表可按登录类型、结果、服务器和来源 IP 筛选。

## 记录内容

| 来源 | 记录 | 来源 IP |
| --- | --- | --- |
| SSH | 用户、认证方式、时间、成功 / 失败 | SSH 日志中的来源地址 |
| Windows RDP | 用户、时间、成功 / 失败 | Security 事件提供时记录 |
| 系统控制台 | 显示“远程 VNC 登录”，不采集或存储用户名 | 留空，显示“不可获取” |
| 服务器 VNC 服务 | 软件类型、时间、成功 / 失败，不存储用户名 | 认证日志明确提供来源时记录 |

“远程 VNC 登录”是按系统控制台登录进行的展示标记，不能区分服务商 VNC / KVM 与物理控制台。打开网页控制台、操作已登录桌面、锁屏解锁、断线重连都不保证产生新登录事件。不会用服务器地址或 `127.0.0.1` 代替未知来源。经过代理、隧道或中继时，日志可能只提供代理地址。

无来源 IP 的事件不会进入来源 IP 失败阈值聚合及国家排行；仍可触发登录成功规则。现有规则根据结果匹配多个协议，历史自定义规则名称保持原样。

## Windows

RDP 使用 Security 4624 / 4625 的 LogonType 10、12；控制台使用 2、11，并排除系统、窗口管理器等账户。不把 Type 3 网络认证失败统称为 RDP 失败，因为它也可能来自 SMB 等服务。因此部分 NLA 阶段失败不在本次采集范围。

系统需在“高级审核策略配置 → 登录/注销 → 审核登录”中启用成功和失败审核。Agent 不修改域策略或本机审核策略；没有生成的审计事件无法补采。重新执行新版 Windows 安装 / 更新命令，会为 `NT SERVICE\SSHGuardAgent` 增加 OpenSSH、Security、Application 三个通道的读取权限。仅替换二进制不会补齐权限。

RealVNC 使用 Application 中 `VNC Server` 的认证记录；UltraVNC 使用 `UltraVNC` 的认证接受 / 拒绝记录。单纯 `connected` / `Connection received` 不算登录。Windows TightVNC 默认查找 `%ProgramData%\TightVNC\tvnserver*.log`，支持 UTF-16LE 日志；应开启能输出 `Authenticated with view-only password` 的调试日志级别。自定义目录需配置路径并授予 Agent 服务账户读取权限。

## Linux 与自定义 VNC 文件

控制台使用 PAM `login` 服务的会话打开和认证失败记录，不把 `sudo`、`su`、`sshd` 会话当作控制台登录。不使用 PAM 且不输出该格式的系统需要额外适配。

journal 同时订阅 SSH、login、Xvnc、Xtigervnc、x0vncserver 和 RealVNC 标识。VNC 文件自动查找 `/root/.vnc/*.log`、`/home/*/.vnc/*.log` 以及对应 `.local/state/tigervnc/*.log`；没有 journald 时还查找 `/var/log/vncserver-x11.log`。Agent 账户必须具有目录遍历及文件读取权限，不会自动放宽用户主目录权限。

在现有 `agent.json` 中加入 `vnc_logs`，保留原有主控地址、凭证等字段，然后重启 Agent。例如：

```json
{
  "vnc_logs": [
    {"path": "/var/log/tigervnc/server.log", "format": "tigervnc"},
    {"path": "/var/log/realvnc/server.log", "format": "realvnc"}
  ]
}
```

Windows 示例路径写作 `C:\\ProgramData\\TightVNC\\tvnserver.log`。`format` 支持 `tigervnc`、`tightvnc`、`realvnc`、`ultravnc`、`auto`；最多 32 个绝对文件路径，不支持通配符。Windows TigerVNC 或非默认安装目录请显式配置。不要同时采集同一服务的文件和事件通道副本，以免重复记录。

当前识别英文标准日志中的已确认标记：TigerVNC 服务端默认像素格式初始化、TightVNC Unix 全控 / 只读认证通过和 Windows 认证完成、RealVNC authenticated、UltraVNC authentication accepted。仅 TCP 接通或协商协议不算成功。版本、语言或日志级别变化可能需要适配；没有客户端地址的成功行保留空 IP，不跨连接猜测地址。首次从文件末尾开始采集，时间优先取日志时间；多行日志沿用最近时间头，首次尚未读到时间头时使用采集时间。

每个日志源独立保存读取位置，共用持久化上传队列。重启后续读、上传重试不重复入库；队列满时不推进读取位置。文件轮转缺口、读取失败或 Windows 日志被清空会报告采集异常。

## 验证

1. 查看服务器的采集状态，确认没有日志权限异常。
2. 新建一次 RDP 或 VNC 会话；控制台需真正进入一次系统登录流程。
3. 在“登录事件”查看类型、时间和来源 IP。控制台记录用户名为空。
4. VNC 没有记录时先检查服务端日志是否输出上述认证标记，以及 Agent 是否能读取；不要仅依据端口连通判断登录成功。

参考：[Windows 4624](https://learn.microsoft.com/windows/security/threat-protection/auditing/event-4624)、[RealVNC 日志格式](https://help.realvnc.com/hc/en-us/articles/4404036540561-Centralising-RealVNC-Server-logs-and-reporting-events-with-Elastic-Stack)、[TigerVNC 源码](https://github.com/TigerVNC/tigervnc)、[TightVNC 源码下载](https://www.tightvnc.com/download.php)、[UltraVNC 源码](https://github.com/ultravnc/UltraVNC)。
