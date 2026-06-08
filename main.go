package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"quota-sentinel/internal/bot"
	"quota-sentinel/internal/checker"
	"quota-sentinel/internal/config"
	"quota-sentinel/internal/handler"
	"quota-sentinel/internal/notifier"
	"quota-sentinel/internal/scheduler"
	"quota-sentinel/internal/server"
	"quota-sentinel/internal/store"
	"quota-sentinel/web"
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

	// seed config file settings into DB (only if DB has no value yet)
	seedSettings(srv.Settings, cfg)

	srv.RegisterAPI(func(api *gin.RouterGroup) {
		api.GET("/sites", siteHandler.List)
		api.POST("/sites", siteHandler.Create)
		api.PUT("/sites/:id", siteHandler.Update)
		api.DELETE("/sites/:id", siteHandler.Delete)

		api.POST("/sites/:id/check", checkHandler.CheckSite)
		api.POST("/check-all", checkHandler.CheckAll)

		api.GET("/settings", settingHandler.GetSettings)
		api.PUT("/settings", settingHandler.UpdateSettings)
		api.POST("/telegram/test", settingHandler.TestTelegram)
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

	sched := scheduler.New(chk, srv.Sites, srv.Thresholds, srv.Settings, notifyFn)
	sched.Start(ctx)
	log.Printf("[scheduler] started with interval from settings")

	// Start Telegram Bot if configured
	var tgBot *bot.Bot
	token, _ := srv.Settings.Get("telegram_bot_token")
	chatIDStr, _ := srv.Settings.Get("telegram_chat_id")
	if token != "" && chatIDStr != "" {
		chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err == nil {
			tgBot, err = bot.New(token, chatID, chk, srv.Sites, srv.Thresholds, srv.Settings)
			if err != nil {
				log.Printf("[bot] failed to start: %v", err)
			} else {
				tgBot.Start(ctx)
				log.Printf("[bot] started")
			}
		}
	}

	httpSrv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: srv.Handler(),
	}

	go func() {
		log.Printf("[server] listening on :%d", cfg.Server.Port)
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
}
