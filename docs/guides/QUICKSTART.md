# 安装与更新

主控支持 Ubuntu / Debian + systemd，amd64 / arm64。

## 安装主控

```sh
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fLsS https://raw.githubusercontent.com/xiuzhenzhi/SSHGuard/main/sshguard.sh -o sshguard.sh
sh sshguard.sh
```

选择「1 安装主控」，输入端口（默认 8080），打开安装器给出的链接创建管理员。默认使用 SQLite。

自定义端口：

```sh
sh sshguard.sh --port 8081
```

进入「服务器管理」添加服务器，复制安装命令到目标机器执行。

找回初始化链接：

```sh
sudo sh /opt/SSHGuard/Server/sshguard.sh --setup-link
```

## 更新中央平台

```sh
sh /opt/SSHGuard/Server/sshguard.sh --update
```

也可打开管理菜单选择「9 更新主控」。更新前备份数据库和配置，备份方法见 [运维手册](../deployment/OPERATIONS.md)。

### 拉取最新更新脚本

重新下载管理脚本并执行更新：

```sh
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fLsS https://raw.githubusercontent.com/xiuzhenzhi/SSHGuard/main/sshguard.sh -o sshguard-update.sh && sh sshguard-update.sh --update
```

只下载脚本：

```sh
curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fLsS https://raw.githubusercontent.com/xiuzhenzhi/SSHGuard/main/sshguard.sh -o sshguard-update.sh
```

### 指定版本

```sh
SSHGUARD_VERSION=v0.3.25 sh /opt/SSHGuard/Server/sshguard.sh --update
```

### 离线更新

下载并校验新发行包，解压后执行：

```sh
sudo sh sshguard-linux-amd64/sshguard.sh --deploy --update
```

ARM64 将目录名中的 `amd64` 换成 `arm64`。

## 管理主控

```sh
sudo sh /opt/SSHGuard/Server/sshguard.sh
```

| 选项 | 操作 |
|---|---|
| 1 | 安装 |
| 2 | 查看状态与访问地址 |
| 3 | 查看日志 |
| 4 | 重启 |
| 5 / 6 | 启动 / 停止 |
| 7 | 卸载，保留数据 |
| 8 | 查看初始化链接 |
| 9 | 更新主控，已安装时默认选择 |

常用命令：

```sh
sudo sh /opt/SSHGuard/Server/sshguard.sh --status
sudo sh /opt/SSHGuard/Server/sshguard.sh --logs
sudo sh /opt/SSHGuard/Server/sshguard.sh --restart
sudo sh /opt/SSHGuard/Server/sshguard.sh --uninstall
```

## 可选部署

- [宝塔 / Nginx / OpenResty 反代与 HTTPS](../deployment/REVERSE-PROXY.md)
- [手工部署与外部数据库](../deployment/INSTALL-NATIVE.md)
- [Docker Compose](../deployment/INSTALL-COMPOSE.md)
