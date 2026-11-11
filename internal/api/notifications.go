package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"sync"
	"time"

	"sshguard/internal/notify"
)

type delivery struct {
	id, lease, kind, ciphertext string
	attempts                    int
	message                     notify.Message
}

// Claims are short SQL transactions; network delivery never holds database locks.
// Leases and compare-and-swap completion allow safe restart and multiple workers.
func (a *App) claimDelivery(ctx context.Context) (*delivery, error) {
	tx, e := a.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	d := &delivery{}
	var channelID, alertID string
	e = tx.QueryRowContext(ctx, a.db.Bind(`SELECT id,channel_id,alert_id,attempts FROM notification_jobs WHERE next_attempt_at<=? AND (status='pending' OR (status='sending' AND locked_until<=?)) ORDER BY priority DESC,next_attempt_at,id LIMIT 1 FOR UPDATE`), now, now).Scan(&d.id, &channelID, &alertID, &d.attempts)
	if e == sql.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var enabled bool
	var last sql.NullTime
	if e = tx.QueryRowContext(ctx, a.db.Bind(`SELECT kind,config_ciphertext,enabled,last_attempt_at FROM notification_channels WHERE id=? FOR UPDATE`), channelID).Scan(&d.kind, &d.ciphertext, &enabled, &last); e != nil {
		return nil, e
	}
	if !enabled || d.attempts >= 8 {
		reason := "channel disabled"
		if d.attempts >= 8 {
			reason = "delivery attempts exhausted"
		}
		if _, e = tx.ExecContext(ctx, a.db.Bind(`UPDATE notification_jobs SET status='dead',last_error=?,locked_until=NULL,lease_token='' WHERE id=?`), reason, d.id); e != nil {
			return nil, e
		}
		return nil, tx.Commit()
	}
	interval := time.Second
	if d.kind == "wecom" || d.kind == "dingtalk" || d.kind == "feishu" {
		interval = 3 * time.Second
	}
	if last.Valid && now.Before(last.Time.Add(interval)) {
		if _, e = tx.ExecContext(ctx, a.db.Bind(`UPDATE notification_jobs SET status='pending',next_attempt_at=?,locked_until=NULL,lease_token='' WHERE id=?`), last.Time.Add(interval), d.id); e != nil {
			return nil, e
		}
		return nil, tx.Commit()
	}
	if e = tx.QueryRowContext(ctx, a.db.Bind(`SELECT title,message,created_at FROM alerts WHERE id=?`), alertID).Scan(&d.message.Title, &d.message.Text, &d.message.At); e != nil {
		return nil, e
	}
	d.lease = token()
	d.message.ID = d.id
	// Bind the encryption context to the channel, never the delivery job.
	plain, e := notify.Open(a.cfg.EncryptionKey, channelID, d.ciphertext)
	if e != nil {
		if _, e = tx.ExecContext(ctx, a.db.Bind(`UPDATE notification_jobs SET status='dead',last_error='channel decryption failed; restore the encryption key',locked_until=NULL,lease_token='' WHERE id=?`), d.id); e != nil {
			return nil, e
		}
		return nil, tx.Commit()
	}
	d.ciphertext = string(plain) // decrypted only in memory for this attempt
	d.attempts++
	if _, e = tx.ExecContext(ctx, a.db.Bind(`UPDATE notification_jobs SET status='sending',attempts=?,locked_until=?,lease_token=? WHERE id=?`), d.attempts, now.Add(time.Minute), d.lease, d.id); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, a.db.Bind(`UPDATE notification_channels SET last_attempt_at=? WHERE id=?`), now, channelID); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return d, nil
}
func (a *App) finishDelivery(ctx context.Context, d *delivery, sendError error) error {
	now := time.Now().UTC()
	status, message := "sent", ""
	var sent any = now
	next := now
	if sendError != nil {
		status = "pending"
		if d.attempts >= 8 {
			status = "dead"
		}
		// Sender returns redacted errors. A bounded generic message avoids future transport errors leaking secrets.
		message = "delivery failed; verify channel credentials, TLS, destination policy and provider status"
		sent = nil
		next = now.Add(time.Duration(min(3600, 30*(1<<min(d.attempts-1, 7)))) * time.Second)
	}
	_, e := a.db.Exec(ctx, `UPDATE notification_jobs SET status=?,last_error=?,sent_at=?,next_attempt_at=?,locked_until=NULL,lease_token='' WHERE id=? AND status='sending' AND lease_token=?`, status, message, sent, next, d.id, d.lease)
	if e == nil {
		a.hub.publish()
	}
	return e
}
func (a *App) deliveryLoop(ctx context.Context) {
	sender := notify.Sender{AllowedHosts: a.cfg.NotificationAllowedHosts}
	for ctx.Err() == nil {
		claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		d, e := a.claimDelivery(claimCtx)
		cancel()
		if e != nil && ctx.Err() == nil {
			log.Print("Notification queue claim failed")
		}
		if d != nil {
			var config notify.Config
			e = json.Unmarshal([]byte(d.ciphertext), &config)
			d.ciphertext = ""
			sendCtx, finish := context.WithTimeout(ctx, 20*time.Second)
			if e == nil {
				e = sender.Send(sendCtx, d.kind, config, d.message)
			}
			finish()
			// Preserve the result even during graceful shutdown; a crash falls back to lease expiry.
			saveCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
			if err := a.finishDelivery(saveCtx, d, e); err != nil {
				log.Print("Notification result save failed; lease will expire")
			}
			done()
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (a *App) StartWorkers(ctx context.Context) func() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); a.agentReleaseLoop(ctx) }()
	wg.Add(1)
	go func() { defer wg.Done(); a.monitorLoop(ctx) }()
	wg.Add(1)
	go func() { defer wg.Done(); a.retentionLoop(ctx) }()
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.deliveryLoop(ctx) }()
	}
	return wg.Wait
}
