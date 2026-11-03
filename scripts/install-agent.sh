#!/bin/sh
# Ubuntu/Debian: systemd. Alpine: OpenRC. No bash, Python, Node or container.
set -eu
umask 077
RELEASE_VERSION='0.3.20'
die() { printf '[错误] %s\n' "$*" >&2; exit 1; }
case "${1:-}" in
    --help|-h) printf '安装：sudo sh install-agent.sh ORIGIN ONE_TIME_TOKEN [--allow-insecure-http]\n更新：sudo sh install-agent.sh ORIGIN --update [--allow-insecure-http]\n离线卸载：sudo sh /opt/SSHGuard/Agent/sshguard/uninstall-agent.sh\n'; exit 0 ;;
esac
[ "$(id -u)" -eq 0 ] || die '请使用 root 或 sudo 运行安装命令。'
[ "$#" -ge 2 ] && [ "$#" -le 3 ] || die 'Usage: install-agent.sh ORIGIN ONE_TIME_TOKEN|--update [--allow-insecure-http]'
endpoint=${1%/}
mode=install
enrollment=$2
allow_http=false
if [ "$#" -eq 3 ]; then
    [ "$3" = --allow-insecure-http ] || die '未知参数，请使用 --help 查看用法。'
    allow_http=true
fi
printf '%s' "$endpoint" | grep -Eq '^https?://([A-Za-z0-9.-]+|\[[A-Fa-f0-9:]+\])(:[0-9]{1,5})?$' || die '主控地址格式不正确，请使用 http(s)://IP或域名:端口。'
download_proto='=https'
case "$endpoint" in
    http://*) "$allow_http" || die 'HTTP 地址需加 --allow-insecure-http，或改用 HTTPS。'; download_proto='=http'; printf '直连 HTTP：传输未加密，请确保主控和 Agent 之间的网络可信。\n' ;;
esac
release_version=${SSHGUARD_EXPECTED_VERSION:-$RELEASE_VERSION}
[ "${#release_version}" -le 32 ] && printf '%s' "$release_version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$' || die '版本号无效，请重新复制控制台安装命令。'
artifact_base="https://github.com/xiuzhenzhi/SSHGuard/releases/download/agent-v$release_version"
artifact_proto='=https'
if [ "${SSHGUARD_TEST_DOWNLOADS:-}" = true ]; then
    [ "${GITHUB_ACTIONS:-}" = true ] || die 'Test download source requires the disposable CI environment.'
    case "$endpoint" in https://localhost:*|https://guard.sshguard.test|https://guard.sshguard.test:*|http://127.0.0.1:*) ;; *) die 'Unsupported test download origin.' ;; esac
    artifact_base="$endpoint/downloads"
    artifact_proto=$download_proto
fi
if [ "$enrollment" = --update ]; then mode=update
else printf '%s' "$enrollment" | grep -Eq '^[a-f0-9]{64}$' || die '安装凭证无效，请在控制台重新生成命令。'; fi
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then init=systemd
elif command -v rc-service >/dev/null 2>&1 && command -v supervise-daemon >/dev/null 2>&1; then init=openrc
else die '需要运行中的 systemd 或 OpenRC 服务管理器。'; fi
printf '\nSSHGuard Agent · 安装与更新\n[1/4] 检查环境（%s）\n' "$init"
if ! command -v curl >/dev/null 2>&1 || [ ! -s /etc/ssl/certs/ca-certificates.crt ]; then
    if command -v apt-get >/dev/null 2>&1; then apt-get update; apt-get install -y curl ca-certificates
    elif command -v apk >/dev/null 2>&1; then apk add --no-cache curl ca-certificates
    else die '请先安装 curl 和 ca-certificates，再重新运行本命令。'; fi
fi
command -v sha256sum >/dev/null 2>&1
case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) die '仅支持 amd64 / arm64 架构。' ;; esac
for path in /opt/SSHGuard /opt/SSHGuard/Agent /opt/SSHGuard/Agent/sshguard-agent /opt/SSHGuard/Agent/agent.json /opt/SSHGuard/Agent/sshguard-agent-bin /etc/systemd/system/sshguard-agent.service /etc/init.d/sshguard-agent; do
    [ ! -L "$path" ] || die "Refusing symlink: $path"
done
if [ -e /usr/local/bin/sshguard-agent ] || [ -e /etc/sshguard/agent.json ]; then die '请先使用旧版卸载脚本卸载 Agent，再重新安装。'; fi
config=/opt/SSHGuard/Agent/agent.json
exe=/opt/SSHGuard/Agent/sshguard-agent-bin
if [ "$mode" = update ]; then
    [ -f "$config" ] && [ -f "$exe" ] || die '尚未安装 Agent，请在控制台复制安装命令。'
fi
mkdir /run/sshguard-agent-install.lock 2>/dev/null || die '另一安装正在执行；确认无安装进程后，可移除 /run/sshguard-agent-install.lock 重试。'
tmp=''
rollback=false
was_active=false
service_stop() { if [ "$init" = systemd ]; then systemctl stop sshguard-agent; else rc-service sshguard-agent stop; fi; }
service_start() { if [ "$init" = systemd ]; then systemctl start sshguard-agent; else rc-service sshguard-agent start; fi; }
service_active() { if [ "$init" = systemd ]; then systemctl is-active --quiet sshguard-agent; else rc-service sshguard-agent status >/dev/null 2>&1; fi; }
write_service() {
if [ "$init" = systemd ]; then
    cat > /etc/systemd/system/sshguard-agent.service <<'UNIT'
[Unit]
Description=SSHGuard Agent
After=network-online.target
Wants=network-online.target
[Service]
User=sshguard
Group=sshguard
ExecStart=/opt/SSHGuard/Agent/sshguard-agent-bin -config /opt/SSHGuard/Agent/agent.json
Restart=on-failure
RestartSec=10
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/opt/SSHGuard/Agent/sshguard-agent
UMask=0077
[Install]
WantedBy=multi-user.target
UNIT
    systemctl daemon-reload
    systemctl enable sshguard-agent
else
    cat > /etc/init.d/sshguard-agent <<'OPENRC'
#!/sbin/openrc-run
description="SSHGuard Agent"
supervisor="supervise-daemon"
command="/opt/SSHGuard/Agent/sshguard-agent-bin"
command_args="-config /opt/SSHGuard/Agent/agent.json"
command_user="sshguard:sshguard"
respawn_delay=10
respawn_max=0
output_logger="logger -t sshguard-agent"
error_logger="logger -t sshguard-agent"
depend() { need net; after firewall; }
OPENRC
    chmod 0755 /etc/init.d/sshguard-agent
    rc-update add sshguard-agent default

fi
}
configure_updater() {
    for path in /opt/SSHGuard/Agent/sshguard /opt/SSHGuard/Agent/sshguard/update-agent.sh /opt/SSHGuard/Agent/sshguard/update-loop.sh /etc/init.d/sshguard-agent-update /etc/systemd/system/sshguard-agent-update.service /etc/systemd/system/sshguard-agent-update.timer; do
        [ ! -L "$path" ] || die "Refusing symlink: $path"
    done
    mkdir -p /opt/SSHGuard/Agent/sshguard
    chown root:root /opt/SSHGuard/Agent/sshguard
    chmod 0700 /opt/SSHGuard/Agent/sshguard
    [ ! -L /opt/SSHGuard/Agent/sshguard/uninstall-agent.sh ] || die 'Refusing symlink uninstaller.'
    cp "$tmp/uninstall-agent.sh" /opt/SSHGuard/Agent/sshguard/uninstall-agent.sh
    chmod 0700 /opt/SSHGuard/Agent/sshguard/uninstall-agent.sh
    # Keep a trusted local installer. Never fetch scripts in a remote task.
    cp "$0" "$tmp/update-agent.sh"
    chmod 0700 "$tmp/update-agent.sh"
    mv -f "$tmp/update-agent.sh" /opt/SSHGuard/Agent/sshguard/update-agent.sh
    if [ "$init" = systemd ]; then
        cat > /etc/systemd/system/sshguard-agent-update.service <<'UNIT'
[Unit]
Description=SSHGuard fixed Agent updater (no remote commands)
After=network-online.target
[Service]
Type=oneshot
ExecStart=/opt/SSHGuard/Agent/sshguard-agent-bin -poll-update -config /opt/SSHGuard/Agent/agent.json
TimeoutStartSec=9min
UMask=0077
UNIT
        cat > /etc/systemd/system/sshguard-agent-update.timer <<'UNIT'
[Unit]
Description=Check administrator-requested SSHGuard Agent update
[Timer]
OnBootSec=60s
OnUnitInactiveSec=60s
[Install]
WantedBy=timers.target
UNIT
        systemctl daemon-reload
        systemctl enable --now sshguard-agent-update.timer
    else
        # OpenRC uses a separate scheduler process; collector remains unprivileged.
        cat > /etc/init.d/sshguard-agent-update <<'OPENRC'
#!/sbin/openrc-run
description="SSHGuard fixed Agent updater"
supervisor="supervise-daemon"
command="/bin/sh"
command_args="/opt/SSHGuard/Agent/sshguard/update-loop.sh"
respawn_delay=10
depend() { need net; }
OPENRC
        cat > /opt/SSHGuard/Agent/sshguard/update-loop.sh <<'LOOP'
#!/bin/sh
while sleep 60; do /opt/SSHGuard/Agent/sshguard-agent-bin -poll-update -config /opt/SSHGuard/Agent/agent.json; done
LOOP
        chmod 0700 /opt/SSHGuard/Agent/sshguard/update-loop.sh
        chmod 0755 /etc/init.d/sshguard-agent-update
        rc-update add sshguard-agent-update default
        if ! rc-service sshguard-agent-update status >/dev/null 2>&1; then rc-service sshguard-agent-update start; fi
    fi
}
prepare_collection() {
    mkdir -p /opt/SSHGuard /opt/SSHGuard/Agent
    chown root:root /opt/SSHGuard
    chmod 0755 /opt/SSHGuard
    chown root:sshguard /opt/SSHGuard/Agent
    chmod 0710 /opt/SSHGuard/Agent
    mkdir -p /opt/SSHGuard/Agent/sshguard-agent
    chown sshguard:sshguard /opt/SSHGuard/Agent/sshguard-agent
    chmod 0700 /opt/SSHGuard/Agent/sshguard-agent
    for group in adm systemd-journal; do
        if grep -q "^$group:" /etc/group; then
            if command -v usermod >/dev/null 2>&1; then usermod -a -G "$group" sshguard
            else addgroup sshguard "$group"; fi
        fi
    done
    # Alpine's syslog runs with wheel/027. Do not grant the Agent wheel access.
    # A setgid directory keeps new/rotated auth files in the dedicated group.
    if [ "$init" = openrc ] && [ -f /etc/alpine-release ] && [ -x /etc/init.d/syslog ] && [ "$(readlink -f /sbin/syslogd)" = /bin/busybox ]; then
        if [ -f /etc/conf.d/syslog ] && grep '^SYSLOGD_OPTS=' /etc/conf.d/syslog | grep -Eq -- '-[fOCR]'; then
            echo 'Custom Alpine syslog options detected; configure auth/authpriv logging readable by sshguard manually.' >&2
            return
        fi
        for path in /etc/syslog.conf /opt/SSHGuard/Agent/logs /opt/SSHGuard/Agent/logs/auth.log; do
            [ ! -L "$path" ] || die "Refusing symlink: $path"
        done
        mkdir -p /opt/SSHGuard/Agent/logs
        chown root:sshguard /opt/SSHGuard/Agent/logs
        chmod 2750 /opt/SSHGuard/Agent/logs
        touch /opt/SSHGuard/Agent/logs/auth.log
        chown root:sshguard /opt/SSHGuard/Agent/logs/auth.log
        chmod 0640 /opt/SSHGuard/Agent/logs/auth.log
        changed=false
        if [ ! -f /etc/syslog.conf ]; then
            printf '*.* /var/log/messages\n' > "$tmp/syslog.conf"
        else
            cp /etc/syslog.conf "$tmp/syslog.conf"
        fi
        if ! grep -Fxq 'auth,authpriv.* /opt/SSHGuard/Agent/logs/auth.log' "$tmp/syslog.conf"; then
            printf '\nauth,authpriv.* /opt/SSHGuard/Agent/logs/auth.log\n' >> "$tmp/syslog.conf"
        fi
        if ! cmp -s "$tmp/syslog.conf" /etc/syslog.conf; then
            chown root:root "$tmp/syslog.conf"
            chmod 0644 "$tmp/syslog.conf"
            mv -f "$tmp/syslog.conf" /etc/syslog.conf
            changed=true
        fi
        rc-update add syslog default
        if rc-service syslog status >/dev/null 2>&1; then
            if "$changed"; then rc-service syslog restart; fi
        else
            rc-service syslog start
        fi
    fi
}
cleanup() {
    status=$?
    trap - EXIT HUP INT TERM
    if "$rollback"; then
        service_stop || true
        if [ -f "$tmp/previous" ]; then mv -f "$tmp/previous" "$exe"; fi
        if [ -f "$tmp/previous-service" ]; then
            cp -p "$tmp/previous-service" "$service_file"
            if [ "$init" = systemd ]; then systemctl daemon-reload; fi
        fi
        if "$was_active"; then service_start || true; fi
        echo '[错误] 更新失败，已恢复旧程序并保留配置。' >&2
    fi
    [ -z "$tmp" ] || rm -rf -- "$tmp"
    rmdir /run/sshguard-agent-install.lock
    exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
mkdir -p /opt
tmp=$(mktemp -d /opt/.sshguard-agent-install.XXXXXX)
printf '[2/4] 下载并校验 Agent…\n'
curl --proto "$artifact_proto" --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 60 --max-redirs 5 --max-filesize 1048576 -fLsS "$artifact_base/uninstall-agent.sh" -o "$tmp/uninstall-agent.sh"
sh -n "$tmp/uninstall-agent.sh"
curl --proto "$artifact_proto" --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 150 --max-redirs 5 --max-filesize 134217728 --retry 1 -fLsS "$artifact_base/sshguard-agent-linux-$arch" -o "$tmp/agent"
curl --proto "$artifact_proto" --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 30 --max-redirs 5 --max-filesize 4096 -fLsS "$artifact_base/sshguard-agent-linux-$arch.sha256" -o "$tmp/hash"
expected=$(awk '{print $1}' "$tmp/hash")
printf '%s' "$expected" | grep -Eq '^[a-f0-9]{64}$' || die '校验文件无效，请检查下载来源后重试。'
printf '%s  %s\n' "$expected" "$tmp/agent" | sha256sum -c -
chmod 0755 "$tmp/agent"
"$tmp/agent" -version
[ "$("$tmp/agent" -version)" = "SSHGuard $release_version linux/$arch" ] || die '下载版本与目标版本不一致，已停止更新。'
if [ "$mode" = update ]; then
    printf '[3/4] 更新程序，保留服务器身份与配置…\n'
    chown root:sshguard "$config"
    chmod 0640 "$config"
    "$tmp/agent" -check -endpoint "$endpoint" -config "$config"
    prepare_collection
    if cmp -s "$tmp/agent" "$exe"; then
        if ! service_active; then service_start; sleep 2; service_active; fi
        configure_updater
        echo '[完成] 已是最新版本，服务运行正常。'; exit 0
    fi
    if [ "$init" = systemd ]; then service_file=/etc/systemd/system/sshguard-agent.service; else service_file=/etc/init.d/sshguard-agent; fi
    cp -p "$service_file" "$tmp/previous-service"
    cp -p "$exe" "$tmp/previous"
    if service_active; then was_active=true; fi
    rollback=true
    service_stop
    mv -f "$tmp/agent" "$exe"
    write_service
    service_start
    sleep 2
    service_active
    "$exe" -check -endpoint "$endpoint" -config "$config"
    configure_updater
    rollback=false
    printf '[4/4] 服务检查通过\n[完成] Agent 已更新，身份与配置已保留。\n'
    exit 0
fi
if ! id sshguard >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then useradd --system --user-group --no-create-home --shell /usr/sbin/nologin sshguard
    else addgroup -S sshguard; adduser -S -D -H -h /nonexistent -s /sbin/nologin -G sshguard sshguard; fi
fi
prepare_collection
printf '[3/4] 注册服务器并设置开机自启…\n'
printf '%s' "$enrollment" | "$tmp/agent" -enroll -endpoint "$endpoint" -config "$config" -allow-insecure-http="$allow_http"
unset enrollment
chown root:sshguard "$config"
chmod 0640 "$config"
if service_active; then service_stop; fi
mv -f "$tmp/agent" "$exe"
write_service
service_start
sleep 2
service_active
"$exe" -check -endpoint "$endpoint" -config "$config"
configure_updater
printf '[4/4] 服务检查通过\n\n[完成] Agent 已启动，请在控制台确认在线。\n状态：%s\n卸载：sudo sh /opt/SSHGuard/Agent/sshguard/uninstall-agent.sh\n' "$([ "$init" = systemd ] && echo 'systemctl status sshguard-agent' || echo 'rc-service sshguard-agent status')"
