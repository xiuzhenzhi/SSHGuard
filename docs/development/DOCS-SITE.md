# 文档站维护

## 编辑位置

| 内容 | 文件 |
|---|---|
| 正文 | `docs/*.md` |
| 首页 | `docs/index.md` |
| 导航与侧栏 | `docs/.vitepress/config.mts` |
| 样式 | `docs/.vitepress/theme/style.css` |

## 本地预览

在项目根目录执行，PowerShell 使用 `npm.cmd`：

```sh
npm ci
npm run docs:dev
```

检查生产构建：

```sh
npm run docs:build
npm run docs:preview
```

重新构建后需重启预览服务。

## 发布

推送到 `main` 后，**Documentation** 工作流自动部署到 GitHub Pages。普通文档修改不需要增加程序版本。

站点路径为 `/SSHGuard/`，更换域名或仓库名时同步修改配置中的 `base` 和图标路径。

## 文档版本

文档站版本由仓库根目录 `DOCS_VERSION` 管理，从 0.4.0 起独立递增，不跟随主控或 Agent。版本显示在站点标题中；修改内容和版本后运行链接检查及生产构建，再推送 main，由 Documentation 工作流发布。
