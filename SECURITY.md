# 安全策略

## 支持范围

仅维护 v0.3.24 及之后版本的安装、配置和升级路径。更早版本不再提供兼容支持。

## 报告漏洞

使用 GitHub Security → **Report a vulnerability** 提交私密报告。未启用该功能时联系维护者。

附上版本、复现步骤和脱敏日志，不公开令牌、密码或真实服务器信息。

## 部署设置

- 公网访问使用 HTTPS，配置方法见 [反代与 HTTPS](docs/deployment/REVERSE-PROXY.md)。
- 验证码和登录封禁见 [登录防护](docs/guides/LOGIN-SECURITY.md)。
- 数据库、配置和加密密钥一起备份，见 [运维手册](docs/deployment/OPERATIONS.md#备份与恢复)。
- Agent 使用各自的安装凭证；不再使用的服务器应撤销凭证。
