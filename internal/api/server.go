package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sparkle-transcoder/internal/config"
	"sparkle-transcoder/internal/media"
	"sparkle-transcoder/internal/task"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	log "github.com/sirupsen/logrus"
)

type Server struct {
	cfg     *config.Config
	scanner *media.Scanner
	tasks   *task.Store
	echo    *echo.Echo
}

func New(cfg *config.Config, scanner *media.Scanner, tasks *task.Store) *Server {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	e.Use(middleware.Logger())
	e.Use(middleware.Gzip())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept},
	}))
	s := &Server{cfg: cfg, scanner: scanner, tasks: tasks, echo: e}
	s.routes()
	return s
}

func (s *Server) Start() error {
	return s.echo.Start(s.cfg.Addr)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}

func (s *Server) routes() {
	s.echo.Static("/output", s.cfg.Output)
	api := s.echo.Group("/api")
	api.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	api.GET("/config", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"mediaRoot":       s.cfg.MediaRoot,
			"output":          s.cfg.Output,
			"incrementalScan": s.cfg.IncrementalScan,
			"scanInterval":    s.cfg.ScanInterval.String(),
			"encoders":        s.cfg.Encoders(),
			"videoExt":        s.cfg.VideoExt,
			"quality":         s.cfg.ConstantQuality,
			"audioKbps":       s.cfg.AudioKbps,
			"enableEncode":    s.cfg.EnableEncode,
			"enableSprite":    s.cfg.EnableSprite,
		})
	})
	api.GET("/scan", func(c echo.Context) error {
		return c.JSON(http.StatusOK, s.scanner.Status())
	})
	api.POST("/scan", func(c echo.Context) error {
		req := struct {
			Force *bool `json:"force"`
		}{}
		if err := c.Bind(&req); err != nil {
			return err
		}
		force := req.Force != nil && *req.Force
		go func() {
			if _, err := s.scanner.Scan(context.Background(), force); err != nil && !errors.Is(err, media.ErrScanRunning) {
				log.Errorf("scan failed: %v", err)
			}
		}()
		return c.JSON(http.StatusAccepted, s.scanner.Status())
	})
	api.GET("/media", s.listMedia)
	api.GET("/media/:id", s.getMedia)
	api.GET("/media/:id/poster", s.getPoster)
	api.GET("/media/:id/fanart", s.getFanart)
	api.GET("/tasks", s.listTasks)
	api.POST("/tasks/refresh", s.refreshTasks)
	api.GET("/tasks/:id", s.getTask)
	api.POST("/tasks", s.createTask)
	api.POST("/tasks/:id/cancel", s.cancelTask)
	api.POST("/tasks/:id/retry", s.retryTask)
}

func (s *Server) listMedia(c echo.Context) error {
	q := strings.ToLower(strings.TrimSpace(c.QueryParam("q")))
	kind := c.QueryParam("kind")
	library := c.QueryParam("library")
	items := s.scanner.Items()
	filtered := make([]media.Item, 0, len(items))
	for _, item := range items {
		if kind != "" && string(item.Kind) != kind {
			continue
		}
		if library != "" && item.Library != library {
			continue
		}
		if q != "" {
			haystack := strings.ToLower(item.Title + " " + item.Show + " " + item.FileName + " " + item.Library)
			if !strings.Contains(haystack, q) {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"items":  filtered,
		"count":  len(filtered),
		"status": s.scanner.Status(),
	})
}

func (s *Server) getMedia(c echo.Context) error {
	item, ok := s.scanner.Get(c.Param("id"))
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "media not found")
	}
	return c.JSON(http.StatusOK, item)
}

func (s *Server) getPoster(c echo.Context) error {
	item, ok := s.scanner.Get(c.Param("id"))
	if !ok || item.Poster == nil {
		return echo.NewHTTPError(http.StatusNotFound, "poster not found")
	}
	return s.safeFile(c, item.Poster.Path)
}

func (s *Server) getFanart(c echo.Context) error {
	item, ok := s.scanner.Get(c.Param("id"))
	if !ok || item.Fanart == nil {
		return echo.NewHTTPError(http.StatusNotFound, "fanart not found")
	}
	return s.safeFile(c, item.Fanart.Path)
}

func (s *Server) safeFile(c echo.Context, path string) error {
	if err := media.ValidateUnderRoot(s.cfg.MediaRoot, path); err != nil {
		return echo.NewHTTPError(http.StatusForbidden, "path outside media root")
	}
	stat, err := os.Stat(path)
	if err != nil || stat.IsDir() {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "public, max-age=300")
	return c.File(path)
}

func (s *Server) listTasks(c echo.Context) error {
	tasks, err := s.tasks.List(task.ListFilter{State: c.QueryParam("state")})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"tasks": tasks, "count": len(tasks), "status": s.tasks.Status()})
}

func (s *Server) refreshTasks(c echo.Context) error {
	go func() {
		if err := s.tasks.Refresh(context.Background()); err != nil {
			log.Errorf("task refresh failed: %v", err)
		}
	}()
	return c.JSON(http.StatusAccepted, s.tasks.Status())
}

func (s *Server) getTask(c echo.Context) error {
	t, err := s.tasks.Read(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "task not found")
	}
	return c.JSON(http.StatusOK, t)
}

func (s *Server) createTask(c echo.Context) error {
	req := task.CreateRequest{}
	if err := c.Bind(&req); err != nil {
		return err
	}
	item, ok := s.scanner.Get(req.MediaID)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "media not found; run a scan first")
	}
	t, err := s.tasks.Create(c.Request().Context(), item, req.Params)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, t)
}

func (s *Server) cancelTask(c echo.Context) error {
	t, err := s.tasks.Cancel(c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, t)
}

func (s *Server) retryTask(c echo.Context) error {
	t, err := s.tasks.Retry(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, t)
}

func StaticURLForOutput(id, name string) string {
	return "/output/" + filepath.ToSlash(filepath.Join(id, name))
}

func StartPeriodicScan(ctx context.Context, scanner *media.Scanner, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := scanner.Scan(ctx, false); err != nil && !errors.Is(err, media.ErrScanRunning) {
					log.Errorf("periodic scan failed: %v", err)
				}
			}
		}
	}()
}
