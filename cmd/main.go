package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/okppop/watchd/config"
	"github.com/okppop/watchd/server"
	"go.uber.org/zap"
)

// main loads configuration, serves one movie, and cleans up on shutdown.
func main() {
	// config
	c := config.Config{}
	flag.StringVar(&c.Path, "path", "", "video file path")
	flag.StringVar(&c.Listen, "listen", ":8080", "listen address")
	flag.StringVar(&c.Password, "password", "", "web password(empty is allowed)")
	flag.StringVar(&c.LogLevel, "log", "info", "log level")
	flag.Parse()

	err := c.Validate()
	if err != nil {
		log.Fatal("failed to validate config:", err.Error())
	}
	// logger
	loggerConfig := zap.NewProductionConfig()
	loggerConfig.DisableStacktrace = false
	loggerConfig.Encoding = "console"
	loggerConfig.Level = c.GetLogLevel()

	logger, err := loggerConfig.Build()
	if err != nil {
		log.Fatal("failed to create logger:", err.Error())
	}
	// http server
	httpServer, err := server.NewServer(c, logger)
	if err != nil {
		logger.Error("failed to initialize server", zap.Error(err))
		return
	}

	defer func() {
		if err := httpServer.Close(); err != nil {
			logger.Warn("failed to clean up media", zap.Error(err))
		}
	}()

	httpListener, err := net.ListenTCP("tcp", c.GetTCPAddr())
	if err != nil {
		logger.Error("failed to listen address", zap.Error(err))
		return
	}

	serveErrors := make(chan error, 1)
	go func() {
		err := httpServer.Serve(httpListener)
		if errors.Is(err, http.ErrServerClosed) {
			return
		}

		if err != nil {
			logger.Error("http server closed unexpectedly", zap.Error(err))
			serveErrors <- err
		}
	}()
	// wait for signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)

	logger.Info("watchd is ready", zap.String("address", httpListener.Addr().String()), zap.String("path", c.Path))
	select {
	case signal := <-ch:
		logger.Info("receive signal", zap.Any("signal", signal))
	case <-serveErrors:
	}

	signal.Stop(ch)

	cleanCtx, cancelClean := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelClean()

	err = httpServer.Shutdown(cleanCtx)
	if err != nil {
		logger.Warn("failed to shutdown http server", zap.Error(err))
	}

	logger.Info("clean success, exit")
}
