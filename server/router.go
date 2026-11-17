package server

import (
	"context"
	"crypto/rand"
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/okppop/watchd/config"
	"github.com/okppop/watchd/media"
	"github.com/okppop/watchd/ui"
	"go.uber.org/zap"
)

type Server struct {
	*http.Server
	media       *media.Manager
	room        *Room
	cancel      context.CancelFunc
	op          sync.Mutex
	connections sync.Map
}

// NewServer initializes the single-file room and serves the embedded React application.
func NewServer(cfg config.Config, logger *zap.Logger) (*Server, error) {
	manager, err := media.NewManager(cfg.Path)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Server{
		media:  manager,
		room:   NewRoom(manager.Source.Duration()),
		cancel: cancel,
	}

	token := rand.Text()

	router := gin.New()
	router.Use(gin.Recovery(), LoggerMiddleware(logger))
	router.POST("/api/login", func(c *gin.Context) {
		var body struct {
			Password string `json:"password"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		if c.ShouldBindJSON(&body) != nil {
			c.JSON(400, gin.H{"error": "Invalid login request"})
			return
		}

		if body.Password != cfg.Password {
			c.JSON(403, gin.H{"error": "Incorrect password"})
			return
		}

		c.SetSameSite(http.SameSiteStrictMode)
		c.SetCookie(AUTH_COOKIE, token, 0, "/", "", c.Request.TLS != nil, true)
		c.Status(204)
	})

	roomGroup := router.Group("/room")
	roomGroup.Use(AuthMiddleware(cfg.Password, token))
	roomGroup.GET("/info", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"source":       manager.Source,
			"duration":     manager.Source.Duration(),
			"capabilities": manager.Capabilities,
			"room":         s.room.Snapshot(manager.Snapshot()),
		})
	})
	roomGroup.GET("/source", func(c *gin.Context) {
		c.File(manager.Path)
	})
	roomGroup.GET("/ws", func(c *gin.Context) {
		s.socket(ctx, c)
	})
	roomGroup.POST("/transcode", func(c *gin.Context) {
		var options media.Options
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
		if c.ShouldBindJSON(&options) != nil {
			c.JSON(400, gin.H{"error": "Invalid transcode settings"})
			return
		}

		if _, err := media.Arguments(manager.Source, options, manager.Capabilities); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		s.op.Lock()
		defer s.op.Unlock()

		s.room.ResetTranscode()
		if err := manager.Start(options); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(202, manager.Snapshot())
	})
	roomGroup.DELETE("/transcode", func(c *gin.Context) {
		s.op.Lock()
		defer s.op.Unlock()

		manager.Cancel()
		c.JSON(200, manager.Snapshot())
	})
	roomGroup.GET("/transcode/:generation/:file", func(c *gin.Context) {
		generation, _ := strconv.Atoi(c.Param("generation"))
		if generation == 0 || generation != manager.Snapshot().Generation {
			c.Status(404)
			return
		}

		name := c.Param("file")

		allowed := name == "index.m3u8" || name == "subtitles.vtt"
		if strings.HasPrefix(name, "segment-") && strings.HasSuffix(name, ".ts") {
			_, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "segment-"), ".ts"))
			allowed = err == nil
		}

		if !allowed || filepath.Base(name) != name {
			c.Status(404)
			return
		}

		switch {
		case strings.HasSuffix(name, ".m3u8"):
			c.Header("Content-Type", "application/vnd.apple.mpegurl")
			c.Header("Cache-Control", "no-store")
		case strings.HasSuffix(name, ".vtt"):
			c.Header("Content-Type", "text/vtt; charset=utf-8")
		case strings.HasSuffix(name, ".ts"):
			c.Header("Content-Type", "video/mp2t")
		}

		c.File(filepath.Join(manager.Dir, name))
	})

	assets, err := fs.Sub(ui.Assets, "dist")
	if err != nil {
		cancel()
		_ = manager.Close()
		return nil, err
	}

	static := http.FileServer(http.FS(assets))
	router.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet ||
			strings.HasPrefix(c.Request.URL.Path, "/room") ||
			strings.HasPrefix(c.Request.URL.Path, "/api") {
			c.Status(404)
			return
		}

		static.ServeHTTP(c.Writer, c.Request)
	})
	s.Server = &http.Server{
		Addr:              cfg.Listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
}

// socket uses one reader and one writer per connection, with periodic authoritative snapshots.
func (s *Server) socket(ctx context.Context, c *gin.Context) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
	}

	connection, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	id := rand.Text()

	name := strings.TrimSpace(c.Query("name"))
	if len(name) > 64 {
		name = name[:64]
	}

	s.room.Connect(id, name, c.Request.RemoteAddr)
	s.connections.Store(connection, true)
	defer func() {
		s.connections.Delete(connection)
		s.room.Disconnect(id)
		_ = connection.Close()
	}()

	connection.SetReadLimit(16384)

	messages := make(chan any, 8)

	closed := make(chan struct{})
	go func() {
		defer close(closed)

		for {
			_ = connection.SetReadDeadline(time.Now().Add(15 * time.Second))

			var message Message
			if err := connection.ReadJSON(&message); err != nil {
				return
			}

			var response any
			if message.Type == "ping" {
				response = gin.H{
					"type":       "pong",
					"clientTime": message.ClientTime,
					"serverTime": time.Now().UnixMilli(),
				}
			} else if err := s.room.Command(id, message, s.media.Snapshot()); err != nil {
				if errors.Is(err, errViewerGone) {
					return
				}

				response = gin.H{
					"type":  "error",
					"error": err.Error(),
				}
			}

			if response != nil {
				select {
				case messages <- response:
				default:
				}
			}
		}
	}()
	_ = connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := connection.WriteJSON(gin.H{
		"type": "welcome",
		"id":   id,
	}); err != nil {
		return
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		var message any
		select {
		case <-ctx.Done():
			return
		case <-closed:
			return
		case message = <-messages:
		case <-ticker.C:
			message = gin.H{
				"type": "state",
				"room": s.room.Snapshot(s.media.Snapshot()),
			}
		}

		_ = connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := connection.WriteJSON(message); err != nil {
			return
		}
	}
}

// Close terminates upgraded connections before deleting media used by child processes.
func (s *Server) Close() error {
	s.cancel()
	s.connections.Range(func(key, value any) bool {
		_ = key.(*websocket.Conn).Close()
		return true
	})
	return s.media.Close()
}
