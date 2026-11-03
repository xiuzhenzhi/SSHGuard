# SSHGuard 文档

## 安装与管理

| 文档 | 内容 |
|---|---|
| [快速开始](guides/QUICKSTART.md) | 安装、更新、管理命令 |
| [安装流程](guides/INSTALLATION-FLOW.md) | 初始化与接入服务器 |
| [原生部署](deployment/INSTALL-NATIVE.md) | 二进制与 systemd |
| [Compose 部署](deployment/INSTALL-COMPOSE.md) | Docker 安装与更新 |
| [Agent 管理](guides/AGENTS.md) | 接入、更新、卸载 |
| [卸载 Agent](guides/UNINSTALL.md) | Windows、systemd、OpenRC 手动卸载 |
| [反代与 HTTPS](deployment/REVERSE-PROXY.md) | 宝塔、Nginx、OpenResty |
| [运维手册](deployment/OPERATIONS.md) | 日志、备份、恢复 |
| [故障排查](guides/TROUBLESHOOTING.md) | 常见问题与处理方法 |

## 功能指南

| 文档 | 内容 |
|---|---|
| [告警与通知](guides/ALERTING.md) | 检测规则、通知渠道、告警确认 |
| [地理位置](guides/GEOIP.md) | 公网 IP、来源国家与排行 |
| [登录防护](guides/LOGIN-SECURITY.md) | 验证码、IP 封禁 |

## 开发与参考

- [开发与测试](development/DEVELOPMENT.md) · [API 参考](development/API.md) · [传输协议](development/TRANSPORT.md)
- [构建与发布](development/RELEASING.md) · [文档站维护](development/DOCS-SITE.md)
- [发布说明](RELEASE-NOTES.md) · [更新日志](../CHANGELOG.md) · [验证记录](development/VALIDATION.md)
- [参与开发](../CONTRIBUTING.md) · [安全策略](../SECURITY.md) · [产品路线](development/ROADMAP.md)

[数据库与数据保留](guides/DATABASE.md)：查看占用、回收 SQLite 空间及设置保留天数。
