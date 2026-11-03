package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"sshguard/internal/storage"
)

// This private manifest lives in StateDirectory, not in the release/web tree.
type setupRecord struct {
	Driver        string `json:"driver"`
	DatabaseURL   string `json:"database_url"`
	AdminUsername string `json:"admin_username"`
	AdminID       string `json:"admin_id"`
	PasswordHash  string `json:"password_hash"`
}

func readSetupRecord(path string) (setupRecord, error) {
	var record setupRecord
	info, err := os.Lstat(path)
	if err != nil {
		return record, err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 {
		return record, fmt.Errorf("invalid setup manifest")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	err = json.Unmarshal(data, &record)
	if err == nil && (record.Driver == "" || record.DatabaseURL == "" || record.AdminID == "") {
		err = fmt.Errorf("incomplete setup manifest")
	}
	return record, err
}

func writeSetupRecord(path string, record setupRecord) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("setup manifest already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

type setupInput struct {
	Driver     string `json:"driver"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Database   string `json:"database"`
	DBUsername string `json:"db_username"`
	DBPassword string `json:"db_password"`
	TLS        bool   `json:"tls"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

func (in setupInput) connection(c Config) (setupRecord, error) {
	if in.Driver == "" {
		in.Driver = "sqlite"
	}
	record := setupRecord{Driver: in.Driver, AdminUsername: strings.TrimSpace(in.Username)}
	if in.Driver == "sqlite" {
		record.DatabaseURL = "file:" + filepath.ToSlash(filepath.Join(filepath.Dir(c.SetupFile), "sshguard.db"))
		return record, nil
	}
	if (in.Driver != "mysql" && in.Driver != "postgres") || in.Host == "" || len(in.Host) > 253 || strings.ContainsAny(in.Host, "/\\@?#\r\n\x00") || in.Port < 1 || in.Port > 65535 || in.Database == "" || len(in.Database) > 64 || in.DBUsername == "" || len(in.DBUsername) > 128 || len(in.DBPassword) > 1024 {
		return record, fmt.Errorf("请检查数据库类型、主机、端口、库名和用户名")
	}
	address := net.JoinHostPort(in.Host, fmt.Sprint(in.Port))
	if in.Driver == "mysql" {
		cfg := mysql.NewConfig()
		cfg.Net = "tcp"
		cfg.Addr = address
		cfg.DBName = in.Database
		cfg.User = in.DBUsername
		cfg.Passwd = in.DBPassword
		if in.TLS {
			cfg.TLSConfig = "true"
		}
		record.DatabaseURL = cfg.FormatDSN()
	} else {
		u := url.URL{Scheme: "postgres", Host: address, Path: "/" + in.Database, User: url.UserPassword(in.DBUsername, in.DBPassword)}
		q := url.Values{"connect_timeout": {"10"}, "sslmode": {"disable"}}
		if in.TLS {
			q.Set("sslmode", "verify-full")
		}
		u.RawQuery = q.Encode()
		record.DatabaseURL = u.String()
	}
	return record, nil
}

type Setup struct {
	cfg       Config
	mu        sync.Mutex
	completed bool
	done      chan struct{}
}

func NewSetup(c Config) *Setup         { return &Setup{cfg: c, done: make(chan struct{})} }
func (s *Setup) Done() <-chan struct{} { return s.done }

func (s *Setup) Router() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	a := &App{cfg: s.cfg}
	r.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
		c.Next()
	})
	r.GET("/api/v1/health", func(c *gin.Context) { ok(c, gin.H{"status": "setup_required", "version": Version}) })
	r.GET("/api/v1/setup/status", func(c *gin.Context) { s.mu.Lock(); defer s.mu.Unlock(); ok(c, gin.H{"required": !s.completed}) })
	auth := func(c *gin.Context) {
		if !a.validBrowserOrigin(c) {
			bad(c, 403, "请求来源不匹配")
			return
		}
		if subtle.ConstantTimeCompare([]byte(hash(c.GetHeader("X-Setup-Token"))), []byte(hash(s.cfg.SetupToken))) != 1 {
			bad(c, 403, "初始化口令无效，请使用安装终端提供的链接或口令")
			return
		}
		c.Next()
	}
	r.POST("/api/v1/setup/test", auth, s.test)
	r.POST("/api/v1/setup/complete", auth, s.complete)
	// No login, Agent, update or application routes exist until initialization ends.
	r.NoRoute(a.serveWeb)
	return r
}

func setupDecode(c *gin.Context) (setupInput, bool) {
	var in *setupInput
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil || in == nil {
		bad(c, 400, "请检查初始化表单")
		return setupInput{}, false
	}
	if decoder.Decode(new(any)) != io.EOF {
		bad(c, 400, "初始化请求只能包含一个 JSON 对象")
		return setupInput{}, false
	}
	return *in, true
}

func setupTables(ctx context.Context, db *storage.DB) (int, error) {
	query := `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`
	if db.Driver == "mysql" {
		query = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()`
	}
	if db.Driver == "postgres" {
		query = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=current_schema()`
	}
	var n int
	err := db.Row(ctx, query).Scan(&n)
	return n, err
}

func (s *Setup) open(ctx context.Context, record setupRecord) (*storage.DB, error) {
	db, err := storage.Open(ctx, record.Driver, record.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("连接失败，请检查地址、数据库、账号密码、TLS 和网络权限")
	}
	intent, e := readSetupRecord(s.cfg.SetupFile + ".pending")
	if e == nil && intent.Driver == record.Driver && intent.DatabaseURL == record.DatabaseURL {
		return db, nil
	}
	n, err := setupTables(ctx, db)
	if err != nil || n != 0 {
		db.Close()
		return nil, fmt.Errorf("请选择一个专用空数据库；不会覆盖已有数据库")
	}
	return db, nil
}

func (s *Setup) test(c *gin.Context) {
	in, valid := setupDecode(c)
	if !valid {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.completed {
		bad(c, 409, "初始化已完成")
		return
	}
	record, err := in.connection(s.cfg)
	if err != nil {
		bad(c, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	db, err := s.open(ctx, record)
	if err != nil {
		bad(c, 400, err.Error())
		return
	}
	db.Close()
	ok(c, gin.H{"connected": true})
}

func (s *Setup) complete(c *gin.Context) {
	in, valid := setupDecode(c)
	if !valid {
		return
	}
	record, err := in.connection(s.cfg)
	if err != nil {
		bad(c, 400, err.Error())
		return
	}
	if len(record.AdminUsername) < 3 || len(record.AdminUsername) > 64 || len(in.Password) < 12 || len(in.Password) > 72 {
		bad(c, 400, "管理员用户名需 3–64 字节，密码需 12–72 字节")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.completed {
		bad(c, 409, "初始化已完成")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	db, err := s.open(ctx, record)
	if err != nil {
		bad(c, 400, err.Error())
		return
	}
	defer db.Close()
	pending := s.cfg.SetupFile + ".pending"
	intent, err := readSetupRecord(pending)
	if err == nil {
		if intent.Driver != record.Driver || intent.DatabaseURL != record.DatabaseURL || intent.AdminUsername != record.AdminUsername || bcrypt.CompareHashAndPassword([]byte(intent.PasswordHash), []byte(in.Password)) != nil {
			bad(c, 409, "上次初始化未完成，请使用相同配置重试")
			return
		}
		record = intent
	} else if errors.Is(err, os.ErrNotExist) {
		encrypted, e := bcrypt.GenerateFromPassword([]byte(in.Password), 12)
		if e != nil {
			bad(c, 500, "无法创建管理员")
			return
		}
		record.AdminID = uuid.NewString()
		record.PasswordHash = string(encrypted)
		if e = writeSetupRecord(pending, record); e != nil {
			bad(c, 500, "无法写入初始化配置，请检查数据目录权限及磁盘空间")
			return
		}
	} else {
		bad(c, 500, "初始化记录无法读取，请检查数据目录")
		return
	}
	if err = storage.Migrate(ctx, db); err != nil {
		bad(c, 500, "数据库初始化失败，请检查专用数据库权限后重试")
		return
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		bad(c, 500, "数据库暂不可用")
		return
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		bad(c, 500, "管理员状态无法读取")
		return
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, db.Bind(`INSERT INTO users(id,username,password_hash,role) VALUES(?,?,?,'admin')`), record.AdminID, record.AdminUsername, record.PasswordHash)
	} else {
		var stored string
		err = tx.QueryRowContext(ctx, db.Bind(`SELECT password_hash FROM users WHERE id=? AND username=? AND role='admin'`), record.AdminID, record.AdminUsername).Scan(&stored)
		if err == nil && (count != 1 || stored != record.PasswordHash) {
			err = sql.ErrNoRows
		}
	}
	if err != nil {
		bad(c, 409, "数据库已被其他账号初始化，拒绝覆盖")
		return
	}
	if err = tx.Commit(); err != nil {
		bad(c, 500, "管理员创建失败，请重试")
		return
	}
	if _, err = os.Lstat(s.cfg.SetupFile); !errors.Is(err, os.ErrNotExist) {
		bad(c, 409, "初始化配置已存在，拒绝覆盖")
		return
	}
	if err = os.Rename(pending, s.cfg.SetupFile); err != nil {
		bad(c, 500, "配置保存失败，可使用相同表单安全重试")
		return
	}
	s.completed = true
	ok(c, gin.H{"completed": true})
	// Let the response flush before the narrow setup server shuts down.
	go func() { time.Sleep(500 * time.Millisecond); close(s.done) }()
}
