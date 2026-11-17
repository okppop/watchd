package config

import (
	"errors"
	"fmt"
	"net"
	"os"

	"go.uber.org/zap"
)

type Config struct {
	Path     string
	Listen   string // listen address
	Password string
	LogLevel string
}

// Validate checks startup arguments before allocating server resources.
func (c Config) Validate() error {
	if c.Path == "" {
		return errors.New("path is empty")
	}

	stat, err := os.Stat(c.Path)
	if err != nil {
		return err
	}

	if !stat.Mode().IsRegular() {
		return errors.New("path must be a regular video file")
	}

	if c.Listen == "" {
		return errors.New("listen address is empty")
	}

	_, err = net.ResolveTCPAddr("tcp", c.Listen)
	if err != nil {
		return err
	}

	switch c.LogLevel {
	case "":
		return errors.New("log level is empty")
	case "debug", "DEBUG":
	case "info", "INFO":
	case "warn", "WARN", "warning", "WARNING":
	case "error", "ERROR":
	default:
		return fmt.Errorf("unknown log level %s", c.LogLevel)
	}

	return nil
}

// GetLogLevel translates the configured log level for zap.
func (c Config) GetLogLevel() zap.AtomicLevel {
	switch c.LogLevel {
	case "debug", "DEBUG":
		return zap.NewAtomicLevelAt(zap.DebugLevel)
	case "info", "INFO":
		return zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn", "WARN", "warning", "WARNING":
		return zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error", "ERROR":
		return zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		return zap.NewAtomicLevelAt(zap.InfoLevel)
	}
}

// GetTCPAddr returns the listen address after successful validation.
func (c Config) GetTCPAddr() *net.TCPAddr {
	tcpAddr, _ := net.ResolveTCPAddr("tcp", c.Listen)
	return tcpAddr
}
