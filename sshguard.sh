#!/bin/sh
# SSHGuard: one entry for installation, updates, service management and removal.
set -eu

# Plain output in redirected logs; modest colors only in interactive terminals.
message() {
    tone=$1; label=$2; shift 2
    if [ -t 1 ] && [ -z "${NO_COLOR:-}" ] && [ "${TERM:-}" != dumb ]; then
        printf '\033[%sm%s\033[0m %s\n' "$tone" "$label" "$*"
    else printf '%s %s\n' "$label" "$*"; fi
}
die() { message 31 '[错误]' "$*" >&2; exit 1; }
installed_program_dir() {
    if [ -f /opt/SSHGuard/Server/sshguard-api ]; then printf /opt/SSHGuard/Server
    fi
}
remove_release_payload() (
    payload=$1
    case "$payload" in /opt/.SSHGuard-stage.??????|/opt/.SSHGuard-backup.??????) ;; *) return 1 ;; esac
    [ -d "$payload" ] && [ ! -L "$payload" ] || return 1
    [ "$(readlink -f "$payload")" = "$payload" ] || return 1
    command -v mountpoint >/dev/null 2>&1 || return 1
    if mountpoint -q "$payload"; then return 1; fi
    unexpected=$(find "$payload" -mindepth 1 -maxdepth 1 ! -name sshguard-api ! -name sshguard-api.service ! -name web ! -name deploy ! -name VERSION ! -name sshguard.sh ! -name deploy.sh ! -name quick-start.sh ! -name update.sh ! -name uninstall.sh -print) || return 1
    [ -z "$unexpected" ] || return 1
    rm -rf --one-file-system -- "$payload"
)
# Only immutable program entries may be replaced or removed; data/config stay put.
program_items='sshguard-api sshguard-api.service web deploy VERSION sshguard.sh deploy.sh quick-start.sh update.sh uninstall.sh'
remove_program_item() (
    item=$1
    case " $program_items " in *" $item "*) ;; *) return 1 ;; esac
    target=/opt/SSHGuard/Server/$item
    [ ! -L /opt/SSHGuard ] && [ ! -L /opt/SSHGuard/Server ] && [ ! -L "$target" ] || return 1
    [ ! -e "$target" ] || { command -v mountpoint >/dev/null 2>&1 && ! mountpoint -q "$target"; } || return 1
    rm -rf --one-file-system -- "$target"
)
management_action() {
    printf '\n  SSHGuard · 主控管理\n  ────────────────────────\n' >&2
    default_choice=1
    installed=$(installed_program_dir)
    if [ -n "$installed" ]; then
        printf '  状态：已安装\n' >&2; default_choice=9
        [ ! -f "$installed/VERSION" ] || printf '  版本：%s\n' "$(cat "$installed/VERSION")" >&2
    else printf '  状态：未安装\n' >&2; fi
    printf '\n  1) 安装主控       9) 更新主控\n  2) 查看状态       3) 查看日志\n  4) 重启服务       5) 启动服务\n  6) 停止服务       8) 初始化链接\n  7) 卸载主控（保留数据）\n  0) 退出\n\n选择 [默认 %s]：' "$default_choice" >&2
    IFS= read -r choice
    case "${choice:-$default_choice}" in
        1) echo --install ;; 9) echo --update-latest ;;
        2) echo --status ;; 3) echo --logs ;; 4) echo --restart ;;
        5) echo --start ;; 6) echo --stop ;; 7) echo --uninstall ;;
        8) echo --setup-link ;; 0) echo exit ;; *) die '请输入 0–9。' ;;
    esac
}
uninstall_platform() (
umask 077
[ "$(id -u)" -eq 0 ] || die '请使用 sudo sh uninstall.sh。'
if [ -e /opt/sshguard ] || [ -L /opt/sshguard ]; then die '旧目录安装请使用旧版卸载脚本卸载后再重新安装。'; fi
for path in /opt/SSHGuard /opt/SSHGuard/Server /etc/systemd/system/sshguard-api.service; do
    [ ! -L "$path" ] || die "拒绝符号链接：$path"
done
unit=/etc/systemd/system/sshguard-api.service
if [ ! -f "$unit" ] && [ ! -d /opt/SSHGuard/Server ]; then printf '未检测到主控安装。\n'; exit 0; fi
case "$*" in
    --yes) [ "$#" -eq 1 ] || die '参数无效。' ;;
    '') printf '卸载主控程序，保留数据库、配置和 Agent。继续？[y/N]: '; IFS= read -r answer; case "$answer" in y|Y) ;; *) printf '已取消。\n'; exit 0 ;; esac ;;
    *) die '用法：sudo sh uninstall.sh [--yes]' ;;
esac
if [ -f "$unit" ]; then
    grep -Fxq 'ExecStart=/opt/SSHGuard/Server/sshguard-api' "$unit" || die '同名服务属于其他安装，停止卸载。'
fi
mkdir /run/sshguard-deploy.lock 2>/dev/null || die '安装或更新正在执行。'
trap 'rmdir /run/sshguard-deploy.lock' EXIT
if [ -f "$unit" ]; then
    systemctl disable --now sshguard-api
    rm -f "$unit"
    systemctl daemon-reload
    systemctl reset-failed sshguard-api 2>/dev/null || true
fi
if [ -d /opt/SSHGuard/Server ]; then
    for item in $program_items; do remove_program_item "$item" || die "无法清理程序文件：$item"; done
    rmdir /opt/SSHGuard/Server 2>/dev/null || true
fi
rmdir /opt/SSHGuard 2>/dev/null || true
printf '主控已卸载；数据库、/opt/SSHGuard/Server 配置与 Agent 保留，已有反代保持原样。\n'
)
deploy_platform() (
umask 077
direct_origin() {
    # Match Komari: display the first address reported by hostname -I.
    # This is display-only; the service listens on its configured interfaces.
    server_ip=$(hostname -I 2>/dev/null | awk '{print $1}') || server_ip=''
    case "$server_ip" in ''|*[!A-Fa-f0-9:.]*) server_ip='' ;; esac
    server_ip=${server_ip:-127.0.0.1}
    case "$server_ip" in *:*) server_ip="[$server_ip]" ;; esac
    printf 'http://%s:%s' "$server_ip" "$1"
}
if [ "${1:-}" = --help ] || [ "${1:-}" = -h ]; then
    printf 'SSHGuard 主控安装与管理\n\n安装：sh deploy.sh [--port 8080] [--config 文件]\n更新到最新正式版：sh deploy.sh --update-latest\n使用当前发行包更新：sh deploy.sh --update（已安装目录中运行则下载最新正式版）\n状态：sh deploy.sh --status\n初始化链接：sh deploy.sh --setup-link\n日志：sh deploy.sh --logs\n重启：sh deploy.sh --restart\n\n默认 IP + 端口直连，SQLite，无需域名、Docker 或 Nginx。\n安装完成后，在网页创建管理员即可接入 Agent。\n可选：--proxy standalone|external|managed；HTTPS / 外部数据库见配置示例。\n'
    exit 0
fi
[ "$(id -u)" -eq 0 ] || die '请使用 sudo sh sshguard.sh 运行。'
if [ "$#" -eq 0 ] && [ -t 0 ]; then
    action=$(management_action)
    [ "$action" != exit ] || exit 0
    set -- "$action"
fi
case "${1:-}" in
    --update-latest) shift; download_release --update "$@"; exit $? ;;
    --uninstall) shift; if [ "$(id -u)" -ne 0 ]; then exec sudo sh "$0" --uninstall "$@"; fi; uninstall_platform "$@"; exit $? ;;
    --status|--logs|--restart|--start|--stop|--setup-link)
        [ "$#" -eq 1 ] && [ -f /opt/SSHGuard/Server/deploy-managed ] || die '未找到向导管理的安装，或参数无效。'
        case "$1" in
            --logs) journalctl -u sshguard-api -n 100 --no-pager; exit $? ;;
            --start) systemctl start sshguard-api ;;
            --stop) systemctl stop sshguard-api ;;
            --restart) systemctl restart sshguard-api; systemctl is-active --quiet sshguard-api || die '启动失败，请查看 --logs。' ;;
        esac
        saved_origin=$(sed -n 's/^PUBLIC_URL="\([^"]*\)"$/\1/p' /opt/SSHGuard/Server/api.env)
        saved_port=$(sed -n 's/^LISTEN_ADDR=".*:\([0-9]*\)"$/\1/p' /opt/SSHGuard/Server/api.env)
        if [ -z "$saved_origin" ]; then
            if grep -Eq '^LISTEN_ADDR="(127\.0\.0\.1|\[::1\]):' /opt/SSHGuard/Server/api.env; then
                saved_origin="http://127.0.0.1:${saved_port:-8080}"
                printf '服务仅监听本机；远程使用反代域名，初始化时保留链接中的 #setup=口令部分。\n'
            else saved_origin=$(direct_origin "${saved_port:-8080}"); fi
        fi
        if [ "$1" = --setup-link ]; then
            grep -Fq 'WEB_SETUP="true"' /opt/SSHGuard/Server/api.env || die '此安装使用配置文件初始化，请使用原管理员账号登录。'
            # The manifest may select an external database; never modify it.
            setup_file=$(sed -n 's/^SETUP_CONFIG_FILE="\([^"]*\)"$/\1/p' /opt/SSHGuard/Server/api.env)
            [ ! -f "${setup_file:-/opt/SSHGuard/Server/data/setup.json}" ] || die '初始化已完成，请直接登录控制台。'
            [ -t 1 ] || die '初始化链接包含口令，请在交互终端直接执行此命令。'
            setup_token=$(sed -n 's/^SETUP_TOKEN="\([^"]*\)"$/\1/p' /opt/SSHGuard/Server/api.env)
            [ -n "$setup_token" ] || die '未找到初始化口令。'
            printf '初始化链接：%s/#setup=%s\n' "$saved_origin" "$setup_token"
            exit 0
        fi
        printf '\nSSHGuard 运行状态\n'
        systemctl show sshguard-api --property=ActiveState,SubState,MainPID --no-pager
        installed=$(installed_program_dir)
        [ ! -f "$installed/VERSION" ] || printf 'Version=%s\n' "$(cat "$installed/VERSION")"
        # Display only non-secret fields; never source the environment file.
        sed -n '/^PUBLIC_URL=/p; /^LISTEN_ADDR=/p; /^DB_DRIVER=/p' /opt/SSHGuard/Server/api.env
        printf '访问地址：%s\n' "$saved_origin"
        printf '日志：sh %s/sshguard.sh --logs\n' "$installed"
        exit 0 ;;
esac
bundle=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
# Installed management entries update from the release channel.
for arg in "$@"; do
    if [ "$arg" = --update ] && [ "$bundle" = "$(installed_program_dir)" ]; then
        download_release "$@"; exit $?
    fi
done
[ -f "$bundle/sshguard-api" ] && [ -f "$bundle/web/index.html" ] || die '本地发行包不完整，请运行 sh sshguard.sh 下载安装。'
version=$(cat "$bundle/VERSION")
printf '%s' "$version" | grep -Eq '^[A-Za-z0-9.+_-]{1,32}$' || die 'Invalid release version.'
command -v apt-get >/dev/null 2>&1 && [ -d /run/systemd/system ] || die '主控需要 Ubuntu / Debian，并启用 systemd。'
mode=install
config_file=''
proxy_arg=''
port_arg=''
while [ "$#" -gt 0 ]; do
    case "$1" in
        --install) shift ;;
        --update) mode=update; shift ;;
        --config) [ "$#" -ge 2 ] || die '--config requires a file'; config_file=$2; shift 2 ;;
        --proxy) [ "$#" -ge 2 ] || die '--proxy requires standalone, external or managed'; proxy_arg=$2; shift 2 ;;
        --port) [ "$#" -ge 2 ] || die '--port 需要指定 1024–65535 的端口'; port_arg=$2; shift 2 ;;
        *) die 'Usage: deploy.sh [--update] [--proxy standalone|external|managed] [--config /path/deploy.conf]' ;;
    esac
done
domain=''; driver=''; dsn=''; certificate=''; private_key=''; email=''; admin_password=''; proxy_mode=''
app_port=''; public_origin=''; builtin_tls=false; initial_setup=false
if [ -n "$config_file" ]; then
    [ -f "$config_file" ] && [ ! -L "$config_file" ] || die 'Configuration must be a regular file.'
    [ "$(stat -c %a "$config_file")" = 600 ] || die 'Set configuration permissions to 600.'
    while IFS= read -r line || [ -n "$line" ]; do
        case "$line" in ''|'#'*) continue ;; esac
        key=${line%%=*}; value=${line#*=}
        case "$key" in
            DOMAIN) domain=$value ;; DB_DRIVER) driver=$value ;; DATABASE_URL) dsn=$value ;;
            TLS_CERT) certificate=$value ;; TLS_KEY) private_key=$value ;; ACME_EMAIL) email=$value ;;
            ADMIN_PASSWORD) admin_password=$value ;; PROXY_MODE) proxy_mode=$value ;; APP_PORT) app_port=$value ;; *) die "Unknown configuration key: $key" ;;
        esac
    done < "$config_file"
fi
prompt() { printf '%s' "$1" >&2; IFS= read -r answer; }
for path in /opt/SSHGuard /opt/SSHGuard/Server /opt/SSHGuard/Server/data /opt/SSHGuard/Server/api.env /opt/SSHGuard/Server/deploy-managed /opt/SSHGuard/Server/proxy-mode /opt/SSHGuard/Server/nginx-existing.conf /opt/SSHGuard/Server/certs /opt/SSHGuard/Server/acme /etc/systemd/system/sshguard-api.service; do
    [ ! -L "$path" ] || die "Refusing symlink: $path"
done
if [ -e /opt/sshguard ] || [ -L /opt/sshguard ]; then die '请先卸载旧安装，再全新安装。'; fi
if [ -f /opt/SSHGuard/Server/sshguard-api ] && [ ! -f /opt/SSHGuard/Server/api.env ]; then die '检测到旧配置布局，请先卸载，再全新安装。'; fi
installed=$(installed_program_dir)
if [ -e /opt/SSHGuard/Server ] && [ -z "$installed" ] && [ ! -f /opt/SSHGuard/Server/deploy-managed ]; then die '/opt/SSHGuard/Server 已存在且不是受管理的安装。'; fi
if [ "$mode" = update ]; then
    [ -n "$installed" ] && [ -f /opt/SSHGuard/Server/deploy-managed ] || die 'No wizard-managed installation found.'
elif [ -n "$installed" ]; then die '主控已安装。请在菜单选择「9 更新主控」，或使用 --update-latest；安装已下载的发行包使用 --update。'; fi
printf '\n============================================================\n'
printf '  SSHGuard %s · 安装与更新\n' "$version"
printf '============================================================\n'
printf '  默认使用 IP + 端口访问，无需域名。\n  安装后打开网页创建管理员，即可接入服务器。\n'
printf '\n[1/5] 检查运行环境\n'
existing_web=false
custom_web=false
if command -v openresty >/dev/null 2>&1; then custom_web=true; fi
for web_binary in /www/server/nginx/sbin/nginx /usr/local/nginx/sbin/nginx /usr/local/openresty/nginx/sbin/nginx /usr/local/openresty/bin/openresty /www/server/openresty/nginx/sbin/nginx; do
    if [ -x "$web_binary" ]; then custom_web=true; fi
done
if command -v nginx >/dev/null 2>&1 || "$custom_web"; then existing_web=true; fi
if command -v ss >/dev/null 2>&1 && ss -H -ltn '( sport = :80 or sport = :443 )' 2>/dev/null | grep -q .; then existing_web=true; fi
if "$existing_web"; then printf '  已有 Web 服务保持原样，SSHGuard 使用独立端口。\n';
else printf '  未检测到现有 Web 服务。\n'; fi
[ -z "$proxy_arg" ] || proxy_mode=$proxy_arg
if [ -f /opt/SSHGuard/Server/api.env ] && [ -f /opt/SSHGuard/Server/deploy-managed ]; then
    saved_proxy=managed
    [ ! -f /opt/SSHGuard/Server/proxy-mode ] || saved_proxy=$(cat /opt/SSHGuard/Server/proxy-mode)
    [ -z "$proxy_mode" ] || [ "$proxy_mode" = "$saved_proxy" ] || die 'Proxy mode changes require explicit migration; updates preserve the installed mode.'
    proxy_mode=$saved_proxy
fi
proxy_mode=${proxy_mode:-standalone}
case "$proxy_mode" in standalone|external|managed) ;; *) die 'PROXY_MODE must be standalone, external or managed.' ;; esac
if [ "$proxy_mode" = managed ]; then
    if "$custom_web"; then
        die '检测到 OpenResty、面板或自定义 Nginx，请使用 --proxy external，避免安装第二套 Nginx。'
    fi
    if "$existing_web" && [ ! -x /usr/sbin/nginx ]; then
        die '已有 Web 服务占用端口，请使用 --proxy external，避免端口冲突。'
    fi
fi
printf '  本次模式：%s\n' "$proxy_mode"
mkdir /run/sshguard-deploy.lock 2>/dev/null || die 'Another deployment is active (or /run/sshguard-deploy.lock is stale).'
switched=false
backup=''
replaced=''
new_items=''
unit_backup=''
stage=''
hidden=false
nginx_changed=false
nginx_backup=''
nginx_existed=false
cleanup() {
    status=$?
    trap - EXIT HUP INT TERM
    if "$hidden"; then stty echo 2>/dev/null || true; fi
    if "$switched" && [ "$status" -ne 0 ]; then
        systemctl stop sshguard-api || true
        for item in $replaced; do
            if [ -e "$backup/$item" ]; then
                remove_program_item "$item" || { echo "无法移除失败的程序文件：$item，已保留恢复文件。" >&2; exit "$status"; }
                mv -T "$backup/$item" "/opt/SSHGuard/Server/$item" || exit "$status"
            else
                case " $new_items " in *" $item "*) remove_program_item "$item" || exit "$status" ;; esac
            fi
        done
        if [ -n "$unit_backup" ]; then cp "$unit_backup" /etc/systemd/system/sshguard-api.service
        else
            systemctl disable sshguard-api 2>/dev/null || true
            rm -f /etc/systemd/system/sshguard-api.service
        fi
        systemctl daemon-reload || true
        if [ "$mode" = update ]; then systemctl restart sshguard-api || true; fi
        echo '[错误] 部署失败，已恢复旧程序和服务配置；数据库迁移保持原样。' >&2
    fi
    if "$nginx_changed" && [ "$status" -ne 0 ]; then
        if "$nginx_existed"; then cp "$nginx_backup" /etc/nginx/conf.d/sshguard.conf
        else rm -f /etc/nginx/conf.d/sshguard.conf; fi
        nginx -t && systemctl reload nginx || true
    fi
    [ -z "$nginx_backup" ] || rm -f "$nginx_backup"
    [ -z "$unit_backup" ] || rm -f "$unit_backup"
    if [ -n "$stage" ] && [ -d "$stage" ]; then remove_release_payload "$stage" || true; fi
    if [ -n "$backup" ] && [ -d "$backup" ]; then remove_release_payload "$backup" || true; fi
    rmdir /run/sshguard-deploy.lock
    exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
export DEBIAN_FRONTEND=noninteractive
apt_updated=false
install_packages() {
    if ! "$apt_updated"; then apt-get update; apt_updated=true; fi
    apt-get install -y "$@"
}
if [ "$mode" = install ]; then
    # Install only missing dependencies; never upgrade a working web stack.
    command -v curl >/dev/null 2>&1 || install_packages curl
    command -v openssl >/dev/null 2>&1 || install_packages openssl
    [ -s /etc/ssl/certs/ca-certificates.crt ] || install_packages ca-certificates
    if [ "$proxy_mode" = managed ] && [ ! -x /usr/sbin/nginx ]; then install_packages nginx; fi
fi
command -v curl >/dev/null 2>&1 || die 'curl is required.'
printf '\n[2/5] 设置应用端口\n'
[ -z "$port_arg" ] || app_port=$port_arg
if [ -f /opt/SSHGuard/Server/api.env ]; then
    [ -f /opt/SSHGuard/Server/deploy-managed ] || die 'Existing api.env is not wizard-managed; migrate explicitly, do not overwrite credentials.'
    public_origin=$(sed -n 's/^PUBLIC_URL="\([^"]*\)"$/\1/p' /opt/SSHGuard/Server/api.env)
    domain=$(printf '%s' "$public_origin" | sed 's|^https\{0,1\}://||; s/:[0-9]*$//')
    app_port=$(sed -n 's/^LISTEN_ADDR=".*:\([0-9]*\)"$/\1/p' /opt/SSHGuard/Server/api.env)
    [ -z "$port_arg" ] || [ "$port_arg" = "$app_port" ] || die '已有安装保留原端口；如需修改，请编辑 /opt/SSHGuard/Server/api.env 后重启。'
    if grep -q '^TLS_CERT_FILE=' /opt/SSHGuard/Server/api.env; then builtin_tls=true; fi
else
    [ "$mode" = install ] || die 'Missing /opt/SSHGuard/Server/api.env.'
    initial_setup=true
    if [ -n "$domain" ]; then
        printf '%s' "$domain" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9.-]*\.[A-Za-z0-9-]+$' || die 'Invalid DOMAIN.'
    fi
    if [ -n "$private_key" ] && [ -z "$certificate" ]; then die 'TLS_KEY requires TLS_CERT.'; fi
    if [ -n "$certificate" ] && [ "$proxy_mode" != external ]; then
        [ -f "$certificate" ] && [ -f "$private_key" ] || die 'TLS_CERT and TLS_KEY must exist before installation.'
    fi
    if [ "$proxy_mode" = managed ] || [ -n "$certificate" ]; then
        if [ -z "$domain" ] && [ -z "$config_file" ]; then prompt 'HTTPS domain: '; domain=$answer; fi
        printf '%s' "$domain" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9.-]*\.[A-Za-z0-9-]+$' || die 'DOMAIN required for explicit HTTPS/proxy mode.'
    fi
    if [ "$proxy_mode" = standalone ]; then
        if [ -n "$certificate" ]; then builtin_tls=true; fi
        if [ -z "$app_port" ] && [ -z "$config_file" ] && [ -t 0 ]; then
            prompt '应用端口 [默认 8080，范围 1024–65535]: '; app_port=${answer:-8080}
        fi
    fi
    app_port=${app_port:-8080}
    case "$app_port" in ''|*[!0-9]*) die 'APP_PORT must be 1024–65535.' ;; esac
    [ "${#app_port}" -le 5 ] && [ "$app_port" -ge 1024 ] && [ "$app_port" -le 65535 ] || die 'APP_PORT must be 1024–65535.'
    if command -v ss >/dev/null 2>&1 && ss -H -ltn "sport = :$app_port" 2>/dev/null | grep -q .; then die "应用端口 $app_port 已被占用，请加 --port 其他端口 后重试（例如 --port 8081）。"; fi
    printf '  应用端口：%s\n' "$app_port"
    public_origin="https://$domain"
    listen_address="127.0.0.1:$app_port"
    if [ "$proxy_mode" = standalone ]; then
        listen_address="0.0.0.0:$app_port"
        if "$builtin_tls"; then public_origin="https://$domain:$app_port"; else public_origin="http://$domain:$app_port"; fi
    fi
    configured_origin=$public_origin
    if [ "$proxy_mode" = external ] && [ -z "$domain" ]; then configured_origin=''; public_origin="http://SERVER_IP:$app_port"; fi
    if [ "$proxy_mode" = standalone ] && ! "$builtin_tls" && [ -z "$domain" ]; then configured_origin=''; public_origin="http://SERVER_IP:$app_port"; fi
    driver=${driver:-sqlite}
    case "$driver" in
        sqlite)
            # Keep writable state outside release directories and inside the
            # private data directory. Do not accept arbitrary filesystem paths.
            [ -z "$dsn" ] || [ "$dsn" = 'file:/opt/SSHGuard/Server/data/sshguard.db' ] || die 'SQLite wizard uses file:/opt/SSHGuard/Server/data/sshguard.db; omit DATABASE_URL.'
            dsn='file:/opt/SSHGuard/Server/data/sshguard.db' ;;
        local-postgres)
            [ -z "$dsn" ] || die 'Use DB_DRIVER=postgres for an existing database.'
            command -v psql >/dev/null 2>&1 || install_packages postgresql
            systemctl enable --now postgresql
            db_password=$(openssl rand -hex 24)
            # Do not alter a pre-existing role/database owned by another installation.
            exists=$(su postgres -s /bin/sh -c "psql -Atqc \"SELECT count(*) FROM pg_roles WHERE rolname='sshguard'\"")
            [ "$exists" = 0 ] || die 'PostgreSQL role sshguard already exists. Configure an external DSN explicitly.'
            printf "CREATE ROLE sshguard LOGIN PASSWORD '%s';\nCREATE DATABASE sshguard OWNER sshguard;\n" "$db_password" | su postgres -s /bin/sh -c 'psql -v ON_ERROR_STOP=1'
            dsn="postgres://sshguard:$db_password@127.0.0.1:5432/sshguard?sslmode=disable"
            unset db_password
            driver=postgres ;;
        postgres|mysql)
            if [ -z "$dsn" ] && [ -z "$config_file" ]; then
                printf 'Database DSN (hidden; create database/user first): ' >&2
                hidden=true; stty -echo; IFS= read -r dsn; stty echo; hidden=false; printf '\n' >&2
            fi
            [ -n "$dsn" ] || die 'Set DATABASE_URL for your existing database.' ;;
        *) die 'DB_DRIVER must be sqlite, local-postgres, postgres or mysql.' ;;
    esac
    [ -n "$admin_password" ] || admin_password=$(openssl rand -hex 20)
    [ "${#admin_password}" -ge 12 ] && [ "${#admin_password}" -le 72 ] || die 'ADMIN_PASSWORD must be 12–72 bytes.'
    mkdir -p /opt/SSHGuard/Server
    # Shared parent for API and Agent: traversal only for other accounts.
    # Secrets remain protected by their own file/subdirectory permissions.
    chown root:root /opt/SSHGuard/Server
    chmod 0755 /opt/SSHGuard/Server
    # systemd EnvironmentFile escaping, never interpreted by a shell.
    env_line() { printf '%s="' "$1"; printf '%s' "$2" | sed 's/\\/\\\\/g; s/"/\\"/g'; printf '"\n'; }
    {
        env_line DB_DRIVER "$driver"; env_line DATABASE_URL "$dsn"; env_line PUBLIC_URL "$configured_origin"
        env_line LISTEN_ADDR "$listen_address"; env_line ADMIN_USERNAME admin; env_line ADMIN_PASSWORD "$admin_password"; env_line GIN_MODE release
        if [ -z "$config_file" ]; then
            env_line WEB_SETUP true; env_line SETUP_TOKEN "$(openssl rand -hex 32)"
            env_line SETUP_CONFIG_FILE /opt/SSHGuard/Server/data/setup.json
        fi
        if [ "$proxy_mode" = standalone ]; then
            if "$builtin_tls"; then
                env_line TLS_CERT_FILE /opt/SSHGuard/Server/tls/fullchain.pem; env_line TLS_KEY_FILE /opt/SSHGuard/Server/tls/privkey.pem
            else env_line ALLOW_INSECURE_HTTP true; fi
        fi
    } > /opt/SSHGuard/Server/api.env
    chmod 0600 /opt/SSHGuard/Server/api.env
    touch /opt/SSHGuard/Server/deploy-managed
    unset dsn admin_password
fi
if [ "$proxy_mode" = managed ]; then
    printf '%s' "$domain" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9.-]*\.[A-Za-z0-9-]+$' || die 'Invalid managed PUBLIC_URL.'
fi
[ -n "$public_origin" ] || public_origin="http://SERVER_IP:$app_port"
printf '%s\n' "$proxy_mode" > /opt/SSHGuard/Server/proxy-mode
chmod 0600 /opt/SSHGuard/Server/proxy-mode
# Upgrade old wizard installations once, preserving the key on every future update.
if ! grep -q '^ENCRYPTION_KEY=' /opt/SSHGuard/Server/api.env; then
    key_file=$(mktemp /opt/SSHGuard/Server/.api-env.XXXXXX)
    chmod 0600 "$key_file"
    cat /opt/SSHGuard/Server/api.env > "$key_file"
    printf '\nENCRYPTION_KEY="%s"\n' "$(openssl rand -hex 32)" >> "$key_file"
    mv -f "$key_file" /opt/SSHGuard/Server/api.env
fi
if ! id sshguard-api >/dev/null 2>&1; then useradd --system --user-group --no-create-home --shell /usr/sbin/nologin sshguard-api; fi
chown root:root /opt/SSHGuard/Server
chmod 0755 /opt/SSHGuard/Server
if [ "$mode" = install ] && [ "$proxy_mode" = standalone ] && "$builtin_tls"; then
    [ -f "$certificate" ] && [ -f "$private_key" ] || die 'TLS_CERT and TLS_KEY must exist.'
    [ ! -L /opt/SSHGuard/Server/tls ] || die 'Refusing symlink: /opt/SSHGuard/Server/tls'
    install -d -o root -g sshguard-api -m 0750 /opt/SSHGuard/Server/tls
    install -o root -g sshguard-api -m 0640 "$certificate" /opt/SSHGuard/Server/tls/fullchain.pem
    install -o root -g sshguard-api -m 0640 "$private_key" /opt/SSHGuard/Server/tls/privkey.pem
fi
[ ! -L /opt/SSHGuard/Server/data ] || die '拒绝数据目录符号链接。'
mkdir -p /opt/SSHGuard
chmod 0755 /opt/SSHGuard
install -d -o sshguard-api -g sshguard-api -m 0700 /opt/SSHGuard/Server/data
# Create private SQLite files explicitly; do not rely only on the service manager's umask.
# Run as the service account so file preparation cannot modify root-owned targets.
if grep -Eq '^(DB_DRIVER="sqlite"|WEB_SETUP="true")$' /opt/SSHGuard/Server/api.env; then
    su sshguard-api -s /bin/sh -c '
        set -eu
        umask 077
        database=/opt/SSHGuard/Server/data/sshguard.db
        [ ! -L "$database" ] || exit 1
        if [ ! -e "$database" ]; then (set -C; : > "$database"); fi
        for path in "$database" "$database-wal" "$database-shm"; do
            [ ! -L "$path" ] || exit 1
            if [ -e "$path" ]; then [ -f "$path" ] && chmod 0600 "$path"; fi
        done
    ' || die '无法准备 SQLite 私有数据文件，请检查数据目录权限。'
fi
stage=$(mktemp -d /opt/.SSHGuard-stage.XXXXXX)
# A bundle may be extracted inside an existing panel website. Copy only release
# payloads, never unrelated site files, deploy.conf or certificate private keys.
for item in sshguard-api web deploy VERSION; do cp -R "$bundle/$item" "$stage/$item"; done
for item in sshguard.sh deploy.sh quick-start.sh update.sh uninstall.sh; do
    [ ! -e "$bundle/$item" ] || cp -R "$bundle/$item" "$stage/$item"
done
# Public binaries/assets must be readable; configuration stays in /etc with 600.
find "$stage" -type d -exec chmod 0755 {} +
find "$stage" -type f -exec chmod 0644 {} +
cp "$stage/deploy/sshguard-api.service" "$stage/sshguard-api.service"
chmod 0755 "$stage/sshguard-api"
printf '\n[3/5] 检查程序与数据库迁移\n'
# Validate database/migrations before switching or stopping the current API.
systemd-run --quiet --wait --pipe --collect --unit="sshguard-check-$$" -p User=sshguard-api -p UMask=0077 -p EnvironmentFile=/opt/SSHGuard/Server/api.env "$stage/sshguard-api" -check
printf '\n[4/5] 配置主控服务（%s）\n' "$proxy_mode"
if [ "$mode" = install ] && [ "$proxy_mode" = managed ]; then
    [ ! -e /etc/nginx/conf.d/sshguard.conf ] || [ -f /opt/SSHGuard/Server/nginx-managed ] || die 'Existing nginx sshguard.conf is not wizard-managed.'
    nginx_backup=$(mktemp /opt/SSHGuard/Server/.nginx-backup.XXXXXX)
    if [ -f /etc/nginx/conf.d/sshguard.conf ]; then nginx_existed=true; cp /etc/nginx/conf.d/sshguard.conf "$nginx_backup"; fi
    nginx_changed=true
    mkdir -p /opt/SSHGuard/Server/certs
    if [ -z "$certificate" ] && [ -z "$config_file" ]; then
        prompt "TLS certificate fullchain path (empty = automatic Let's Encrypt): "; certificate=$answer
        if [ -n "$certificate" ]; then prompt 'TLS private key path: '; private_key=$answer; fi
    fi
    if [ -n "$certificate" ]; then
        [ -f "$certificate" ] && [ -f "$private_key" ] || die 'TLS_CERT and TLS_KEY must exist.'
        openssl x509 -in "$certificate" -noout -checkend 86400 >/dev/null || die 'Certificate invalid or expires in less than one day.'
        cp "$certificate" /opt/SSHGuard/Server/certs/fullchain.pem
        cp "$private_key" /opt/SSHGuard/Server/certs/privkey.pem
        chmod 0600 /opt/SSHGuard/Server/certs/privkey.pem
    else
        if [ -z "$email" ] && [ -z "$config_file" ]; then prompt "Email for Let's Encrypt notifications (ACME terms accepted by deployment): "; email=$answer; fi
        [ -n "$email" ] || die 'Set ACME_EMAIL or TLS_CERT/TLS_KEY.'
        command -v certbot >/dev/null 2>&1 || install_packages certbot
        mkdir -p /opt/SSHGuard/Server/acme
        chmod 0755 /opt/SSHGuard/Server/acme
        printf 'server { listen 80; server_name %s; location /.well-known/acme-challenge/ { root /opt/SSHGuard/Server/acme; } location / { return 404; } }\n' "$domain" > /etc/nginx/conf.d/sshguard.conf
        touch /opt/SSHGuard/Server/nginx-managed
        nginx -t
        systemctl enable --now nginx
        systemctl reload nginx
        certbot certonly --webroot -w /opt/SSHGuard/Server/acme -d "$domain" --email "$email" --agree-tos --non-interactive
        ln -sf "/etc/letsencrypt/live/$domain/fullchain.pem" /opt/SSHGuard/Server/certs/fullchain.pem
        ln -sf "/etc/letsencrypt/live/$domain/privkey.pem" /opt/SSHGuard/Server/certs/privkey.pem
        mkdir -p /etc/letsencrypt/renewal-hooks/deploy
        printf '#!/bin/sh\nnginx -t && systemctl reload nginx\n' > /etc/letsencrypt/renewal-hooks/deploy/sshguard-nginx.sh
        chmod 0755 /etc/letsencrypt/renewal-hooks/deploy/sshguard-nginx.sh
    fi
    sed -e "s/guard.example.com/$domain/g" -e "s/127.0.0.1:8080/127.0.0.1:$app_port/g" "$bundle/deploy/nginx.conf" > /etc/nginx/conf.d/sshguard.conf
    touch /opt/SSHGuard/Server/nginx-managed
    nginx -t
fi
if [ "$proxy_mode" = external ]; then
    sed "s/127.0.0.1:8080/127.0.0.1:$app_port/g" "$bundle/deploy/nginx-existing.conf" > /opt/SSHGuard/Server/nginx-existing.conf
    chmod 0600 /opt/SSHGuard/Server/nginx-existing.conf
    printf '  已生成 /opt/SSHGuard/Server/nginx-existing.conf（兼容 OpenResty）；现有 Web 服务与证书保持原样。\n'
fi
if [ -f /etc/systemd/system/sshguard-api.service ]; then
    unit_backup=$(mktemp /opt/SSHGuard/Server/.service-backup.XXXXXX)
    cp /etc/systemd/system/sshguard-api.service "$unit_backup"
fi
switched=true
systemctl stop sshguard-api 2>/dev/null || { [ "$mode" = install ] || die '无法停止现有服务。'; }
for item in $program_items; do
    target=/opt/SSHGuard/Server/$item
    [ ! -L "$target" ] || die "拒绝程序符号链接：$item"
    if [ -e "$target" ] && mountpoint -q "$target"; then die "拒绝替换挂载点：$item"; fi
done
backup=$(mktemp -d /opt/.SSHGuard-backup.XXXXXX)
for item in $program_items; do
    [ ! -L "/opt/SSHGuard/Server/$item" ] || die "拒绝程序符号链接：$item"
    replaced="$item $replaced"
    if [ -e "/opt/SSHGuard/Server/$item" ]; then
        mv -T "/opt/SSHGuard/Server/$item" "$backup/$item"
    else new_items="$item $new_items"; fi
    if [ -e "$stage/$item" ]; then mv -T "$stage/$item" "/opt/SSHGuard/Server/$item"; fi
 done
cp /opt/SSHGuard/Server/sshguard-api.service /etc/systemd/system/sshguard-api.service
chmod 0644 /etc/systemd/system/sshguard-api.service
systemctl daemon-reload
printf '\n[5/5] 启动服务并检查健康状态\n'
systemctl enable sshguard-api
systemctl restart sshguard-api
ready=false
for attempt in 1 2 3 4 5 6 7 8 9 10; do
    if "$builtin_tls"; then
        health=$(curl -fsS --noproxy '*' --max-time 3 --resolve "$domain:$app_port:127.0.0.1" --cacert /opt/SSHGuard/Server/tls/fullchain.pem "$public_origin/api/v1/health" 2>/dev/null) || health=''
    else health=$(curl -fsS --noproxy '*' --max-time 3 "http://127.0.0.1:$app_port/api/v1/health" 2>/dev/null) || health=''; fi
    if printf '%s' "$health" | grep -Fq "\"version\":\"$version\""; then ready=true; break; fi
    sleep 2
done
"$ready" || die '服务未就绪，请执行 journalctl -u sshguard-api -n 50 查看原因。'
if [ "$proxy_mode" = managed ]; then
    systemctl enable --now nginx
    systemctl reload nginx
fi
switched=false
if [ "$public_origin" = "http://SERVER_IP:$app_port" ]; then
    if [ "$proxy_mode" = external ]; then public_origin="http://127.0.0.1:$app_port"
    else public_origin=$(direct_origin "$app_port"); fi
fi
if [ -n "$backup" ]; then
    remove_release_payload "$backup" || printf '  旧目录含额外文件或无法清理，已保留：%s\n' "$backup" >&2
fi
if [ "$mode" = update ]; then printf '\n更新完成 · SSHGuard %s\n访问地址：%s\n' "$version" "$public_origin"
else printf '\n安装完成 · SSHGuard %s\n访问地址：%s\n' "$version" "$public_origin"; fi
actual_listen=$(sed -n 's/^LISTEN_ADDR="\([^"]*\)"$/\1/p' /opt/SSHGuard/Server/api.env)
printf '实际监听：%s\n' "$actual_listen"
case "$actual_listen" in
    127.0.0.1:*|'[::1]:'*) printf '当前端口仅供本机访问，请通过同机反代域名打开；机器 IP + 此端口不能直接访问。\n' ;;
    *) printf 'IP + 端口可经对应网卡访问；若外部不通，请检查防火墙、安全组和已有映射。\n' ;;
esac
if grep -Fq '"status":"setup_required"' <<EOF
$health
EOF
then
    printf '下一步：打开初始化链接，创建管理员，随后接入第一台服务器。\n默认 SQLite 已就绪；外部数据库可在网页中按需选择。\n'
    if [ -t 1 ] && [ -w /dev/tty ]; then
        setup_token=$(sed -n 's/^SETUP_TOKEN="\([^"]*\)"$/\1/p' /opt/SSHGuard/Server/api.env)
        printf '初始化链接：%s/#setup=%s\n' "$public_origin" "$setup_token" > /dev/tty
        unset setup_token
    else
        printf '请在终端执行 sudo sh /opt/SSHGuard/Server/sshguard.sh --setup-link 查看初始化链接。\n'
    fi
elif grep -Fq 'WEB_SETUP="true"' /opt/SSHGuard/Server/api.env; then
    printf '请使用网页初始化时创建的管理员账号登录。\n'
elif "$initial_setup" && [ -t 1 ] && [ -w /dev/tty ]; then
    # Only show the first-install password on the operator's terminal, never CI/redirected logs.
    initial_password=$(sed -n 's/^ADMIN_PASSWORD="\([^"]*\)"$/\1/p' /opt/SSHGuard/Server/api.env)
    printf '初始密码：%s\n首次登录后请修改密码。\n' "$initial_password" > /dev/tty
    unset initial_password
else
    printf '初始密码保存在 /opt/SSHGuard/Server/api.env 的 ADMIN_PASSWORD 中（已修改的登录密码保持不变）。\n'
fi
if [ "$proxy_mode" = standalone ] && ! "$builtin_tls" && [ "${public_origin#http://}" != "$public_origin" ]; then
    printf '\n请在云安全组 / 防火墙中放行 TCP %s；若使用 NAT，请使用对应公网 IP 与映射端口。\n直连 HTTP 的登录、Agent 上报与下载未加密；可按需配置 HTTPS。\n' "$app_port"
fi
printf '\n一键更新：sh /opt/SSHGuard/Server/sshguard.sh --update\n管理命令：sudo sh /opt/SSHGuard/Server/sshguard.sh\n安装与可选配置：https://github.com/xiuzhenzhi/SSHGuard/blob/main/docs/guides/QUICKSTART.md\n'

)
download_release() (
case "${1:-}" in
    --help|-h)
        printf 'SSHGuard 主控\n\n安装：sh sshguard.sh [--port 8080]\n更新到最新正式版：sh sshguard.sh --update\n指定版本：SSHGUARD_VERSION=v版本号 sh sshguard.sh --update\n管理：--status / --logs / --restart / --start / --stop / --setup-link / --uninstall\n\n默认 IP + 端口、SQLite，无需域名、Docker 或 Nginx。\n支持 Ubuntu / Debian + systemd，amd64 / arm64。\n'
        exit 0 ;;
esac
if [ "$#" -eq 0 ] && [ -t 0 ]; then
    action=$(management_action)
    [ "$action" != exit ] || exit 0
    set -- "$action"
fi
if [ "${1:-}" = --update-latest ]; then shift; set -- --update "$@"; fi
if [ "$#" -eq 0 ] && [ -n "$(installed_program_dir)" ]; then set -- --update; fi
case "${1:-}" in
    --uninstall) shift; if [ "$(id -u)" -ne 0 ]; then exec sudo sh "$0" --uninstall "$@"; fi; uninstall_platform "$@"; exit $? ;;
    --status|--logs|--restart|--start|--stop|--setup-link)
        if [ "$(id -u)" -ne 0 ]; then exec sudo sh "$0" "$@"; fi
        deploy_platform "$@"; exit $? ;;
esac
version=${SSHGUARD_VERSION:-v0.3.25}
for arg in "$@"; do
    if [ "$arg" = --update ] && [ -z "${SSHGUARD_VERSION:-}" ]; then version=latest; fi
done
valid_version() {
    case "$1" in ''|*[!A-Za-z0-9.+_-]*) return 1 ;; esac
    printf '%s' "$1" | grep -Eq '^v[0-9][A-Za-z0-9.+_-]{0,31}$'
}
[ "$version" = latest ] || valid_version "$version" || { echo 'Invalid SSHGUARD_VERSION' >&2; exit 1; }
case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) echo 'Only Linux amd64/arm64 supported.' >&2; exit 1 ;; esac
[ "$(uname -s)" = Linux ] || { echo 'Run on the Ubuntu/Debian central server.' >&2; exit 1; }
for tool in sha256sum tar; do command -v "$tool" >/dev/null 2>&1 || { echo "Required command: $tool" >&2; exit 1; }; done
github_cli=false
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then github_cli=true; fi
if ! "$github_cli"; then command -v curl >/dev/null 2>&1 || { echo 'Required command: curl (or authenticated gh)' >&2; exit 1; }; fi
github_curl() { curl --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 300 --retry 2 -fLsS "$@"; }
if [ "$version" = latest ]; then
    message 36 '[准备]' '查询最新正式版…'
    if "$github_cli"; then
        version=$(gh release view --repo xiuzhenzhi/SSHGuard --json tagName --jq .tagName) || { echo '无法查询最新发行版，请检查 GitHub 登录和仓库访问权限。' >&2; exit 1; }
    else
        latest_url=$(github_curl --head --output /dev/null --write-out '%{url_effective}' https://github.com/xiuzhenzhi/SSHGuard/releases/latest) || { echo '无法查询最新发行版，请检查网络；私有仓库请先执行 gh auth login。' >&2; exit 1; }
        case "$latest_url" in
            https://github.com/xiuzhenzhi/SSHGuard/releases/tag/*) version=${latest_url##*/} ;;
            *) echo '无法解析最新发行版，请检查仓库是否已有正式 Release；私有仓库请先执行 gh auth login。' >&2; exit 1 ;;
        esac
    fi
    valid_version "$version" || { echo 'Invalid release version returned by GitHub.' >&2; exit 1; }
fi
message 36 '[下载]' "SSHGuard $version · Linux/$arch"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
trap 'exit 1' HUP INT TERM
asset="sshguard-linux-$arch.tar.gz"
if "$github_cli"; then
    gh release download "$version" --repo xiuzhenzhi/SSHGuard --pattern "$asset" --pattern SHA256SUMS --dir "$tmp"
else
    base="https://github.com/xiuzhenzhi/SSHGuard/releases/download/$version"
    for name in "$asset" SHA256SUMS; do
        github_curl "$base/$name" -o "$tmp/$name" || {
            echo '[错误] 下载失败，请检查网络；私有仓库请先执行 gh auth login。' >&2
            exit 1
        }
    done
fi
(
    cd "$tmp"
    awk -v name="$asset" '$2==name {print; n++} END {if(n!=1)exit 1}' SHA256SUMS > selected.sha256
    sha256sum -c selected.sha256
    tar -xzf "$asset"
)
bundle="$tmp/sshguard-linux-$arch"
[ "$(cat "$bundle/VERSION")" = "${version#v}" ] || { echo 'Release VERSION does not match the requested tag.' >&2; exit 1; }
if [ -f "$bundle/sshguard.sh" ]; then
    if [ "$(id -u)" -eq 0 ]; then sh "$bundle/sshguard.sh" --deploy "$@"
    else sudo sh "$bundle/sshguard.sh" --deploy "$@"; fi
else
    # Releases published before the unified entry point remain installable.
    if [ "$(id -u)" -eq 0 ]; then sh "$bundle/deploy.sh" "$@"
    else sudo sh "$bundle/deploy.sh" "$@"; fi
fi

)
case "${1:-}" in
    --deploy) shift; deploy_platform "$@" ;;
    --update-latest) shift; download_release --update "$@" ;;
    --status|--logs|--restart|--start|--stop|--setup-link|--uninstall)
        if [ "$(id -u)" -ne 0 ]; then exec sudo sh "$0" "$@"; fi
        download_release "$@" ;;
    *) download_release "$@" ;;
esac
