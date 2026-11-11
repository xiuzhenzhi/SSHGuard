# Docker Compose 部署

需要 Docker Engine、Compose 2.24.4+ 和 Node.js。

默认直接拉取 GitHub Container Registry 中的预构建镜像，支持 Linux amd64 / arm64：

- 主控：`ghcr.io/xiuzhenzhi/sshguard-api:v0.4.0`
- 前端：`ghcr.io/xiuzhenzhi/sshguard-web:v0.4.0`

两者使用相同版本标签；可在 `.env` 设置 `SSHGUARD_IMAGE_TAG=v0.4.0`。镜像由已发布且校验通过的安装包组装，包含对应版本的 Agent 下载文件。

```sh
git clone https://github.com/xiuzhenzhi/SSHGuard.git
cd SSHGuard
```

## 本机体验

选择一种数据库生成配置：

```sh
node scripts/dev/init-env.mjs sqlite http://localhost:8088
# 或将 sqlite 换成 postgres / mysql
docker compose config --quiet
docker compose pull
docker compose up -d --no-build
docker compose ps
```

打开 `http://localhost:8088`。账号为 `admin`，密码见 `.env`。本机模式不用于 Agent 接入。

## 公网 HTTPS

在新的部署目录执行，替换域名与证书路径：

```sh
node scripts/dev/init-env.mjs sqlite https://guard.example.com
mkdir -p deploy/certs
cp /path/to/fullchain.pem deploy/certs/fullchain.pem
cp /path/to/privkey.pem deploy/certs/privkey.pem
chmod 600 deploy/certs/privkey.pem
docker compose config --quiet
docker compose pull
docker compose up -d --no-build
```

已有 `.env` 时直接编辑。从本机模式切换 HTTPS，还需从 `COMPOSE_FILE` 移除 `:deploy/compose/compose.local.yaml`。

## 更新

备份 `.env` 和数据卷，在 `.env` 中将 `SSHGUARD_IMAGE_TAG` 改为需要的已发布版本，然后在原项目目录执行：

```sh
git pull --ff-only
docker compose config --quiet
docker compose pull
docker compose up -d --no-build
docker compose ps
```

保留原数据库类型和加密密钥；原生安装的 `update.sh` 不用于 Compose。

## 从源码构建

开发或自定义修改时，在 `.env` 的 `COMPOSE_FILE` 末尾添加 `:deploy/compose/compose.build.yaml`，然后执行 `docker compose up -d --build`。普通部署无需这个覆盖文件。

## 停止与数据

```sh
docker compose down
```

不要加 `-v`，否则会删除数据卷。备份与恢复见 [运维手册](OPERATIONS.md#备份与恢复)。
