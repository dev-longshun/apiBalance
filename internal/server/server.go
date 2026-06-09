package server

import (
	"fmt"
	"io/fs"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"upstream-balance/internal/config"
	"upstream-balance/internal/store"
)

type Server struct {
	engine *gin.Engine
	cfg    *config.Config
	db     *store.DB

	Sites      *store.SiteStore
	Thresholds *store.ThresholdStore
	Settings   *store.SettingStore
}

func New(cfg *config.Config, db *store.DB) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(cors.Default())

	s := &Server{
		engine:     engine,
		cfg:        cfg,
		db:         db,
		Sites:      store.NewSiteStore(db),
		Thresholds: store.NewThresholdStore(db),
		Settings:   store.NewSettingStore(db),
	}

	return s
}

func (s *Server) RegisterAPI(register func(api *gin.RouterGroup)) {
	api := s.engine.Group("/api")
	register(api)
}

func (s *Server) ServeStaticFS(webFS fs.FS) {
	s.engine.NoRoute(gin.WrapH(http.FileServer(http.FS(webFS))))
}

func (s *Server) Run() error {
	addr := fmt.Sprintf(":%d", s.cfg.Server.Port)
	return s.engine.Run(addr)
}

func (s *Server) Handler() http.Handler {
	return s.engine
}
