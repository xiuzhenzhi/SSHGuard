---
layout: home
title: SSHGuard 文档
hero:
  name: SSHGuard
  text: 让每一次 SSH 登录<br>都有迹可循
  tagline: 主控、Agent 与文档独立版本，按需升级；集中审计 SSH 登录、管理告警与服务器。
  image:
    src: /sshguard.svg
    alt: SSHGuard 盾牌与终端标志
  actions:
    - theme: brand
      text: 开始安装
      link: /docs/guides/QUICKSTART
    - theme: alt
      text: 更新已有主控
      link: /docs/guides/QUICKSTART#更新中央平台
    - theme: alt
      text: 查看 GitHub
      link: https://github.com/xiuzhenzhi/SSHGuard
features:
  - title: 一键安装与更新
    details: 无需域名、Docker 或额外数据库，即可通过 IP 和端口开始使用；已有配置和数据在更新时保留。
    link: /docs/guides/QUICKSTART
    linkText: 部署主控
  - title: 跨平台 SSH 审计
    details: 支持 Windows、Ubuntu、Debian 和 Alpine，集中查看登录时间、账号、来源地址和认证结果。
    link: /docs/guides/AGENTS
    linkText: 接入服务器
  - title: 告警与通知
    details: 关注异常登录、认证失败、Agent 上线、离线和采集异常，将通知送到你使用的渠道。
    link: /docs/guides/ALERTING
    linkText: 配置告警
  - title: 公网 IP 与来源国家
    details: 识别公网 IPv4 / IPv6，在服务器和事件中显示国家与地区，查看认证失败来源国家排行。
    link: /docs/guides/GEOIP
    linkText: 了解地理位置
  - title: 数据由你掌控
    details: 自托管部署，可选 SQLite、MySQL 或 PostgreSQL；配置、数据库和加密密钥都由你维护。
    link: /docs/deployment/OPERATIONS
    linkText: 备份与运维

---
