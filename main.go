package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"upstream-balance/internal/bot"
	"upstream-balance/internal/checker"
	"upstream-balance/internal/config"
	"upstream-balance/internal/forecast"
	"upstream-balance/internal/handler"
	"upstream-balance/internal/notifier"
	"upstream-balance/internal/scheduler"
	"upstream-balance/internal/server"
	"upstream-balance/internal/store"
	"upstream-balance/web"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := store.Open(cfg.Database.Path)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	srv := server.New(cfg, db)
	chk := checker.New()

	siteHandler := handler.NewSiteHandler(srv.Sites, srv.Thresholds, chk)
	checkHandler := handler.NewCheckHandler(srv.Sites, srv.Thresholds, chk)
	settingHandler := handler.NewSettingHandler(srv.Settings, srv.Sites)
	authHandler := handler.NewAuthHandler(srv.Settings)

	// seed config file settings into DB (only if DB has no value yet)
	seedSettings(srv.Settings, cfg)

	srv.RegisterAPI(func(api *gin.RouterGroup) {
		api.POST("/login", authHandler.Login)
		api.POST("/logout", authHandler.Logout)

		api.Use(authHandler.Middleware())

		api.GET("/sites", siteHandler.List)
		api.POST("/sites", siteHandler.Create)
		api.PUT("/sites/:id", siteHandler.Update)
		api.DELETE("/sites/:id", siteHandler.Delete)

		api.POST("/sites/:id/check", checkHandler.CheckSite)
		api.POST("/check-all", checkHandler.CheckAll)

		api.GET("/settings", settingHandler.GetSettings)
		api.PUT("/settings", settingHandler.UpdateSettings)
		api.POST("/telegram/test", settingHandler.TestTelegram)
		api.POST("/email/test", settingHandler.TestEmail)
		api.GET("/status", settingHandler.GetStatus)
	})

	webFS, _ := fs.Sub(web.StaticFS, ".")
	srv.ServeStaticFS(webFS)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	notifyFn := func() *notifier.Telegram {
		token, _ := srv.Settings.Get("telegram_bot_token")
		chatID, _ := srv.Settings.Get("telegram_chat_id")
		return notifier.New(token, chatID)
	}

	emailFn := func() *notifier.Email {
		if !handler.EmailEnabled(srv.Settings) {
			return nil
		}
		return notifier.NewEmail(handler.EmailConfigFromSettings(srv.Settings))
	}

	fc := forecast.New(store.NewUsageStore(db), srv.Settings)
	sched := scheduler.New(chk, srv.Sites, srv.Thresholds, srv.Settings, fc, notifyFn, emailFn)
	sched.Start(ctx)
	log.Printf("[scheduler] started with interval from settings")

	// Start Telegram Bot if configured
	var tgBot *bot.Bot
	token, _ := srv.Settings.Get("telegram_bot_token")
	chatIDStr, _ := srv.Settings.Get("telegram_chat_id")
	if token != "" && chatIDStr != "" {
		var chatIDs []int64
		for _, s := range strings.Split(chatIDStr, ",") {
			s = strings.TrimSpace(s)
			if id, err := strconv.ParseInt(s, 10, 64); err == nil {
				chatIDs = append(chatIDs, id)
			}
		}
		if len(chatIDs) > 0 {
			tgBot, err = bot.New(token, chatIDs, chk, srv.Sites, srv.Thresholds, srv.Settings, fc)
			if err != nil {
				log.Printf("[bot] failed to start: %v", err)
			} else {
				tgBot.Start(ctx)
				log.Printf("[bot] started")
			}
		}
	}

	listenHost := cfg.Server.Host
	if listenHost == "" {
		listenHost = "0.0.0.0"
	}
	listenAddr := net.JoinHostPort(listenHost, strconv.Itoa(cfg.Server.Port))
	httpSrv := &http.Server{
		Addr:    listenAddr,
		Handler: srv.Handler(),
	}

	go func() {
		log.Printf("[server] listening on %s", listenAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[shutdown] graceful shutdown...")
	cancel()
	sched.Stop()
	if tgBot != nil {
		tgBot.Stop()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	httpSrv.Shutdown(shutdownCtx)

	log.Println("[shutdown] done")
}

func seedSettings(settings *store.SettingStore, cfg *config.Config) {
	if cfg.Scheduler.IntervalMinutes > 0 {
		if v, _ := settings.Get("interval_minutes"); v == "30" || v == "" {
			settings.Set("interval_minutes", strconv.Itoa(cfg.Scheduler.IntervalMinutes))
		}
	}
	if cfg.Telegram.BotToken != "" {
		if v, _ := settings.Get("telegram_bot_token"); v == "" {
			settings.Set("telegram_bot_token", cfg.Telegram.BotToken)
		}
	}
	if cfg.Telegram.ChatID != "" {
		if v, _ := settings.Get("telegram_chat_id"); v == "" {
			settings.Set("telegram_chat_id", cfg.Telegram.ChatID)
		}
	}
	// email: only seed keys that have never been set, so edits made in the web panel win.
	if cfg.Email.Enabled {
		if v, _ := settings.Get("email_enabled"); v == "" {
			settings.Set("email_enabled", "true")
		}
	}
	emailSeeds := map[string]string{
		"smtp_host":      cfg.Email.SMTPHost,
		"smtp_security":  cfg.Email.SMTPSecurity,
		"smtp_user":      cfg.Email.SMTPUser,
		"smtp_password":  cfg.Email.SMTPPassword,
		"smtp_from_name": cfg.Email.FromName,
		"email_to":       cfg.Email.To,
	}
	if cfg.Email.SMTPPort > 0 {
		emailSeeds["smtp_port"] = strconv.Itoa(cfg.Email.SMTPPort)
	}
	for k, val := range emailSeeds {
		if val == "" {
			continue
		}
		if v, _ := settings.Get(k); v == "" {
			settings.Set(k, val)
		}
	}
	if cfg.Admin.Username != "" {
		settings.Set("admin_username", cfg.Admin.Username)
	}
	if cfg.Admin.Password != "" {
		settings.Set("admin_password", cfg.Admin.Password)
	}
}
