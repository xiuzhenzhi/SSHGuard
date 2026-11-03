# SSHGuard

集中管理 Windows / Linux 服务器的 SSH 登录记录与安全告警。

## 功能

- SSH 审计：查看登录时间、用户、来源 IP、端口和认证结果。
- 安全告警：异常登录、连续认证失败、服务器离线与采集异常通知。
- 服务器管理：分组、IP 白名单、Agent 安装与远程更新。
- 登录防护：人机验证、登录错误封禁、用户权限与操作审计。

支持 Windows、Ubuntu、Debian、Alpine；数据库可选 SQLite、PostgreSQL 或 MySQL。

![安全总览](docs/images/overview-preview.png)

## 安装

主控使用 Ubuntu / Debian，默认通过 IP + 端口访问，使用 SQLite，无需域名或 Docker。

```sh
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fLsS https://raw.githubusercontent.com/xiuzhenzhi/SSHGuard/main/sshguard.sh -o sshguard.sh
sh sshguard.sh
```

更新主控：

```sh
sh sshguard.sh --update
```

已安装的主控可运行 `sh /opt/SSHGuard/Server/sshguard.sh --update`。管理菜单「1」安装，「9」更新。

## 文档

[在线文档](https://xiuzhenzhi.github.io/SSHGuard/)
