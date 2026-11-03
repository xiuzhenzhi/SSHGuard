package api

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/url"
	"strings"
	"time"
)

const serverColumns = `id,name,hostname,distribution,group_name,notes,agent_version,last_seen_at,created_at,CASE WHEN revoked=true THEN 'revoked' WHEN last_seen_at IS NULL THEN 'pending' WHEN last_seen_at>? THEN 'online' ELSE 'offline' END AS status,(SELECT collector_state FROM agent_health WHERE server_id=servers.id) AS collector_state,(SELECT collector_message FROM agent_health WHERE server_id=servers.id) AS collector_message,(SELECT queue_depth FROM agent_health WHERE server_id=servers.id) AS queue_depth,(SELECT rejected_events FROM agent_health WHERE server_id=servers.id) AS rejected_events,(SELECT status FROM agent_updates WHERE server_id=servers.id) AS update_status,(SELECT updater_seen_at FROM agent_updates WHERE server_id=servers.id) AS updater_seen_at,(SELECT target_version FROM agent_updates WHERE server_id=servers.id) AS update_target,(SELECT expires_at FROM agent_updates WHERE server_id=servers.id) AS update_expires_at,COALESCE((SELECT report_interval_seconds FROM server_settings WHERE server_id=servers.id),5) AS report_interval_seconds,COALESCE((SELECT reported_interval_seconds FROM server_settings WHERE server_id=servers.id),0) AS reported_interval_seconds,(SELECT ip_addresses FROM server_settings WHERE server_id=servers.id) AS ip_addresses`

func (a *App) servers(c *gin.Context) {
	q := "%" + c.Query("q") + "%"
	size, offset := page(c)
	rows, err := a.db.List(c.Request.Context(), `SELECT `+strings.Replace(serverColumns, "last_seen_at>?", a.freshSQL("last_seen_at", "servers.id"), 1)+`,(SELECT public_ip FROM server_network WHERE server_id=servers.id) AS observed_public_ip FROM servers WHERE name LIKE ? OR hostname LIKE ? OR group_name LIKE ? ORDER BY created_at DESC LIMIT ? OFFSET ?`, time.Now().UTC().Unix(), q, q, q, size+1, offset)
	if err != nil {
		fail(c, err)
		return
	}
	more := len(rows) > size
	if more {
		rows = rows[:size]
	}
	for _, row := range rows {
		row["available_agent_version"] = AgentVersion
	}
	a.decorateServers(c.Request.Context(), rows)
	ok(c, gin.H{"items": rows, "has_more": more})
}
func (a *App) server(c *gin.Context) {
	rows, e := a.db.List(c.Request.Context(), `SELECT `+strings.Replace(serverColumns, "last_seen_at>?", a.freshSQL("last_seen_at", "servers.id"), 1)+`,(SELECT public_ip FROM server_network WHERE server_id=servers.id) AS observed_public_ip FROM servers WHERE id=?`, time.Now().UTC().Unix(), c.Param("id"))
	if e != nil {
		fail(c, e)
		return
	}
	if len(rows) == 0 {
		bad(c, 404, "服务器不存在")
		return
	}
	for _, row := range rows {
		row["available_agent_version"] = AgentVersion
	}
	a.decorateServers(c.Request.Context(), rows)
	ok(c, rows[0])
}
func (a *App) saveServer(c *gin.Context) {
	var in struct {
		Name     string `json:"name" binding:"max=128"`
		Group    string `json:"group_name" binding:"max=64"`
		Notes    string `json:"notes" binding:"max=1024"`
		Interval *int   `json:"report_interval_seconds" binding:"omitempty,min=1,max=600"`
	}
	if !bind(c, &in) {
		return
	}
	if in.Interval != nil {
		bad(c, 400, "上报间隔只能在安装时设置")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = "服务器-" + uuid.NewString()[:8]
	}
	id := c.Param("id")
	action := "server.update"
	create := id == ""
	if create {
		id = uuid.NewString()
		action = "server.create"
	}
	e := a.mutate(c, c.MustGet("user").(User).ID, action, id, func(tx *sql.Tx) error {
		if create {
			_, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`INSERT INTO servers(id,name,group_name,notes) VALUES(?,?,?,?)`), id, in.Name, in.Group, in.Notes)
			if e != nil {
				return e
			}
		} else {
			var exists string
			if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT id FROM servers WHERE id=? FOR UPDATE`), id).Scan(&exists); e != nil {
				return e
			}
			if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`UPDATE servers SET name=?,group_name=?,notes=? WHERE id=?`), in.Name, in.Group, in.Notes, id); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		fail(c, e)
		return
	}
	ok(c, gin.H{"id": id, "name": in.Name})
}
func (a *App) deleteServer(c *gin.Context) {
	id := c.Param("id")
	err := a.mutate(c, c.MustGet("user").(User).ID, "server.delete", id, func(tx *sql.Tx) error {
		var existing string
		// Serialize deletion with Agent writes, enrollment and health monitoring.
		if err := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT id FROM servers WHERE id=? FOR UPDATE`), id).Scan(&existing); err != nil {
			return err
		}
		// Remove dependents first; all supported databases enforce these foreign keys.
		for _, query := range []string{
			`DELETE FROM notification_jobs WHERE alert_id IN (SELECT id FROM alerts WHERE server_id=?)`,
			`DELETE FROM alerts WHERE server_id=?`,
			`DELETE FROM ssh_events WHERE server_id=?`,
			`DELETE FROM agent_health WHERE server_id=?`,
			`DELETE FROM agent_updates WHERE server_id=?`,
			`DELETE FROM server_settings WHERE server_id=?`,
			`DELETE FROM server_network WHERE server_id=?`,
			`DELETE FROM agent_install_settings WHERE token_hash IN (SELECT token_hash FROM agent_enrollments WHERE server_id=?)`,
			`DELETE FROM agent_enrollments WHERE server_id=?`,
			`DELETE FROM agent_credentials WHERE server_id=?`,
			`DELETE FROM servers WHERE id=?`,
		} {
			if _, err := tx.ExecContext(c.Request.Context(), a.db.Bind(query), id); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		bad(c, 404, "服务器不存在或已删除")
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	a.dashboardMu.Lock()
	clear(a.dashboardCache)
	a.countryEpoch++
	a.dashboardMu.Unlock()
	a.hub.publish()
	ok(c, gin.H{"deleted": true})
}

func shellQuote(s string) string      { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func powershellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func agentAsset(name string) string {
	return "https://github.com/xiuzhenzhi/SSHGuard/releases/download/" + url.PathEscape("v"+AgentVersion) + "/" + url.PathEscape(name)
}
func installCommands(origin, t string) map[string]string {
	linuxOption, windowsOption := "", ""
	if strings.HasPrefix(origin, "http://") {
		linuxOption, windowsOption = " --allow-insecure-http", " -AllowInsecureHttp"
	}
	linux := func(privilege string) string {
		return linuxAgentCommand("install-agent.sh", " "+shellQuote(origin)+" "+shellQuote(t)+linuxOption, privilege)
	}
	windows := fmt.Sprintf(`$f=Join-Path $env:TEMP ([guid]::NewGuid().ToString()+'.ps1'); $curl=Join-Path ([Environment]::SystemDirectory) 'curl.exe'; $shell=Join-Path ([Environment]::SystemDirectory) 'WindowsPowerShell\v1.0\powershell.exe'; try { & $curl --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 60 --max-redirs 5 --max-filesize 1048576 -fLsS %s -o $f; if ($LASTEXITCODE -ne 0) { throw 'GitHub installer download failed' }; & $shell -NoProfile -File $f -Endpoint %s -EnrollmentToken %s%s; if ($LASTEXITCODE -ne 0) { throw 'Agent installation/update failed' } } finally { if (Test-Path -LiteralPath $f) { Remove-Item -LiteralPath $f } }`, powershellQuote(agentAsset("install-agent.ps1")), powershellQuote(origin), powershellQuote(t), windowsOption)
	return map[string]string{"linux": linux("auto"), "ubuntu": linux("sudo "), "debian": linux("sudo "), "alpine": linux(""), "windows": windows}
}

// Root shells (including Alpine without sudo) run directly. Other shells use
// sudo; the installer itself detects systemd/OpenRC and the CPU architecture.
func linuxAgentCommand(asset, arguments, privilege string) string {
	run := `sh "$f"` + arguments
	if privilege == "auto" {
		run = fmt.Sprintf(`if [ "$(id -u)" -eq 0 ]; then %s; else sudo %s; fi`, run, run)
	} else {
		run = privilege + run
	}
	return fmt.Sprintf(`(f=$(mktemp) && trap 'rm -f "$f"' EXIT && curl --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 60 --max-redirs 5 --max-filesize 1048576 -fLsS %s -o "$f" && %s)`, shellQuote(agentAsset(asset)), run)
}
func (a *App) enrollment(c *gin.Context) {
	var in struct {
		Interval *int `json:"report_interval_seconds" binding:"omitempty,min=1,max=600"`
	}
	if c.Request.ContentLength != 0 && !bind(c, &in) {
		return
	}
	interval := 5
	if in.Interval != nil {
		interval = *in.Interval
	}
	origin, valid := a.agentOrigin(c)
	if !valid {
		return
	}
	id := c.Param("id")
	t := token()
	expires := time.Now().UTC().Add(15 * time.Minute)
	e := a.mutate(c, c.MustGet("user").(User).ID, "agent.enrollment.create", id, func(tx *sql.Tx) error {
		var existing string
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT id FROM servers WHERE id=? FOR UPDATE`), id).Scan(&existing); e != nil {
			return e
		}
		if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`DELETE FROM agent_install_settings WHERE token_hash IN (SELECT token_hash FROM agent_enrollments WHERE server_id=?)`), id); e != nil {
			return e
		}
		if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`DELETE FROM agent_enrollments WHERE server_id=?`), id); e != nil {
			return e
		}
		if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`INSERT INTO agent_enrollments(token_hash,server_id,expires_at) VALUES(?,?,?)`), hash(t), id, expires); e != nil {
			return e
		}
		_, e := tx.ExecContext(c.Request.Context(), a.db.Bind(`INSERT INTO agent_install_settings(token_hash,report_interval_seconds,expires_at) VALUES(?,?,?)`), hash(t), interval, expires)
		return e
	})
	if e != nil {
		fail(c, e)
		return
	}
	commands := installCommands(origin, t)
	ok(c, gin.H{"command": commands["linux"], "commands": commands, "expires_at": expires, "phase": "registration-and-heartbeat-only", "endpoint": origin, "insecure_http": strings.HasPrefix(origin, "http://")})
}
func updateCommands(origin string) map[string]string {
	commands := installCommands(origin, "--update")
	commands["windows"] = strings.Replace(commands["windows"], "-EnrollmentToken '--update'", "-Update", 1)
	return commands
}
func uninstallCommands(origin string) map[string]string {
	linux := func(privilege string) string {
		return linuxAgentCommand("uninstall-agent.sh", "", privilege)
	}
	windows := fmt.Sprintf(`$f=Join-Path $env:TEMP ([guid]::NewGuid().ToString()+'.ps1'); $curl=Join-Path ([Environment]::SystemDirectory) 'curl.exe'; $shell=Join-Path ([Environment]::SystemDirectory) 'WindowsPowerShell\v1.0\powershell.exe'; try { & $curl --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 60 --max-redirs 5 --max-filesize 1048576 -fLsS %s -o $f; if ($LASTEXITCODE -ne 0) { throw 'GitHub uninstaller download failed' }; & $shell -NoProfile -File $f; if ($LASTEXITCODE -ne 0) { throw 'Agent uninstall failed' } } finally { if (Test-Path -LiteralPath $f) { Remove-Item -LiteralPath $f } }`, powershellQuote(agentAsset("uninstall-agent.ps1")))
	return map[string]string{"linux": linux("auto"), "ubuntu": linux("sudo "), "debian": linux("sudo "), "alpine": linux(""), "windows": windows}
}
func (a *App) revoke(c *gin.Context) {
	id := c.Param("id")
	e := a.mutate(c, c.MustGet("user").(User).ID, "agent.revoke", id, func(tx *sql.Tx) error {
		var existing string
		if e := tx.QueryRowContext(c.Request.Context(), a.db.Bind(`SELECT id FROM servers WHERE id=? FOR UPDATE`), id).Scan(&existing); e != nil {
			return e
		}
		for _, q := range []string{`UPDATE servers SET revoked=true WHERE id=?`, `UPDATE agent_credentials SET revoked_at=CURRENT_TIMESTAMP WHERE server_id=?`, `DELETE FROM agent_enrollments WHERE server_id=?`} {
			if _, e := tx.ExecContext(c.Request.Context(), a.db.Bind(q), id); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		fail(c, e)
		return
	}
	ok(c, gin.H{"revoked": true})
}
