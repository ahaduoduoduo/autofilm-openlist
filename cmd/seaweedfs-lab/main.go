// seaweedfs-lab runs the real tier gateway with a disk-backed 115 simulator.
// It is a separate developer executable, never registered in OpenList itself.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/server/seaweedfs"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18790", "loopback listen address")
	dir := flag.String("dir", "", "required persistent lab directory")
	interval := flag.Int64("read-interval-ms", 1000, "minimum interval between new link API requests")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || *dir == "" {
		log.Fatal("an IP loopback address and -dir are required")
	}
	if err := os.MkdirAll(*dir, 0700); err != nil {
		log.Fatal(err)
	}
	database, err := gorm.Open(sqlite.Open(filepath.Join(*dir, "receipts.db")+"?_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatal(err)
	}
	if err := database.AutoMigrate(&model.SeaweedObject{}, &model.SeaweedReadGate{}, &model.SeaweedLink{}); err != nil {
		log.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		log.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	p := &simulator{root: filepath.Join(*dir, "cloud"), base: "http://" + *listen}
	config := conf.SeaweedFS{MaxObjectMiB: 64, ReadIntervalMilliseconds: *interval, CooldownSeconds: 60, RecoveryToken: "local-lab-recovery-token-32-characters"}
	for _, name := range []string{"notes", "photos", "files"} {
		config.Spaces = append(config.Spaces, conf.SeaweedFSSpace{Name: name, Token: "local-lab-" + name + "-token", MountPath: "/fake115", ParentID: name})
	}
	h, err := seaweedfs.NewHandler(config, database, func(path string) (driver.Driver, error) {
		if path != "/fake115" {
			return nil, fmt.Errorf("unknown mount")
		}
		return p, nil
	})
	if err != nil {
		log.Fatal(err)
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	h.Register(r.Group("/seaweedfs/v1"))
	r.GET("/lab/status", func(c *gin.Context) { p.mu.Lock(); defer p.mu.Unlock(); c.JSON(200, p.stats) })
	r.POST("/lab/control", func(c *gin.Context) {
		var control controls
		if json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096)).Decode(&control) != nil {
			c.Status(400)
			return
		}
		p.mu.Lock()
		p.control = control
		if control.ExpireLinks {
			p.rejectBefore = time.Now().UnixNano()
			p.expiredStatus = control.ExpiredStatus
			if p.expiredStatus == 0 {
				p.expiredStatus = http.StatusGone
			}
		}
		p.mu.Unlock()
		c.Status(204)
	})
	r.GET("/cdn/:parent/:name", p.download)
	server := &http.Server{Addr: *listen, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Printf("local simulator and real OpenList gateway: %s", p.base)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
