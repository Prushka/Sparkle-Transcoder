package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
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
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions},
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
	api.GET("/tools", s.toolStatus)
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
	api.DELETE("/tasks/:id", s.deleteTask)
	api.POST("/tasks/delete", s.deleteTasks)
}

type toolSpec struct {
	ID          string
	Name        string
	Command     string
	VersionArgs []string
}

type toolReadiness struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Command  string `json:"command"`
	Path     string `json:"path,omitempty"`
	Ready    bool   `json:"ready"`
	Version  string `json:"version,omitempty"`
	Error    string `json:"error,omitempty"`
	Required bool   `json:"required"`
}

func (s *Server) toolStatus(c echo.Context) error {
	specs := []toolSpec{
		{ID: "ffmpeg", Name: "FFmpeg", Command: s.cfg.Ffmpeg, VersionArgs: []string{"-version"}},
		{ID: "ffprobe", Name: "FFprobe", Command: s.cfg.Ffprobe, VersionArgs: []string{"-version"}},
		{ID: "handbrake", Name: "HandBrakeCLI", Command: s.cfg.HandbrakeCli, VersionArgs: []string{"--version"}},
	}
	tools := make([]toolReadiness, 0, len(specs))
	for _, spec := range specs {
		tools = append(tools, checkTool(c.Request().Context(), spec))
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"tools": tools})
}

func checkTool(parent context.Context, spec toolSpec) toolReadiness {
	status := toolReadiness{
		ID:       spec.ID,
		Name:     spec.Name,
		Command:  spec.Command,
		Required: true,
	}
	path, err := exec.LookPath(spec.Command)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Path = path
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, spec.VersionArgs...)
	out, err := cmd.CombinedOutput()
	status.Version = firstVersionLine(string(out))
	if err != nil {
		if ctx.Err() != nil {
			status.Error = ctx.Err().Error()
		} else {
			status.Error = err.Error()
		}
		return status
	}
	status.Ready = true
	return status
}

func firstVersionLine(output string) string {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 160 {
			return line[:157] + "..."
		}
		return line
	}
	return "Available"
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
	if err := s.tasks.Refresh(c.Request().Context()); err != nil {
		return err
	}
	tasks, err := s.tasks.List(task.ListFilter{State: c.QueryParam("state")})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"tasks": tasks, "count": len(tasks), "status": s.tasks.Status()})
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

func (s *Server) deleteTask(c echo.Context) error {
	if err := s.tasks.Delete(c.Param("id")); err != nil {
		if errors.Is(err, task.ErrTaskRunning) {
			return echo.NewHTTPError(http.StatusConflict, err.Error())
		}
		return err
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"deleted": 1})
}

func (s *Server) deleteTasks(c echo.Context) error {
	req := struct {
		IDs []string `json:"ids"`
	}{}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if len(req.IDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "ids are required")
	}
	type deleteFailure struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	seen := map[string]bool{}
	failures := []deleteFailure{}
	deleted := 0
	for _, id := range req.IDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if err := s.tasks.Delete(id); err != nil {
			failures = append(failures, deleteFailure{ID: id, Error: err.Error()})
			continue
		}
		deleted++
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"requested": len(seen),
		"deleted":   deleted,
		"failures":  failures,
	})
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
