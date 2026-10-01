package utils

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/benoitpetit/voie/internal/brand"
)

type Level int

const (
	INFO Level = iota
	WARN
	ERROR
	DEBUG
)

var (
	colors = map[Level]string{
		INFO:  "\033[36m", // Cyan
		WARN:  "\033[33m", // Yellow
		ERROR: "\033[31m", // Red
		DEBUG: "\033[90m", // Gray
	}
	reset   = "\033[0m"
	logger  *log.Logger
	mu      sync.Mutex
	level   = INFO
	noColor = false
)

func init() {
	logger = log.New(os.Stdout, "", 0)
}

func SetLevel(l Level) {
	level = l
}

func enabled(messageLevel Level) bool {
	if level == DEBUG {
		return true
	}
	if messageLevel == DEBUG {
		return false
	}
	return messageLevel >= level
}

func SetOutput(output io.Writer) {
	if output == nil {
		output = os.Stdout
	}
	mu.Lock()
	logger.SetOutput(output)
	mu.Unlock()
}

func SetNoColor(nc bool) {
	noColor = nc
}

func formatTime() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func formatLog(level Level, format string, v ...interface{}) string {
	levelStr := ""
	switch level {
	case INFO:
		levelStr = "INF"
	case WARN:
		levelStr = "WRN"
	case ERROR:
		levelStr = "ERR"
	case DEBUG:
		levelStr = "DBG"
	}

	color := ""
	if !noColor {
		color = colors[level]
	}

	msg := fmt.Sprintf(format, v...)
	if noColor {
		return fmt.Sprintf("%s | %s | %s", formatTime(), levelStr, msg)
	}
	return fmt.Sprintf("%s%s | %s | %s%s", color, formatTime(), levelStr, msg, reset)
}

func Info(format string, v ...interface{}) {
	if enabled(INFO) {
		mu.Lock()
		logger.Print(formatLog(INFO, format, v...))
		mu.Unlock()
	}
}

func Warn(format string, v ...interface{}) {
	if enabled(WARN) {
		mu.Lock()
		logger.Print(formatLog(WARN, format, v...))
		mu.Unlock()
	}
}

func Error(format string, v ...interface{}) {
	if enabled(ERROR) {
		mu.Lock()
		logger.Print(formatLog(ERROR, format, v...))
		mu.Unlock()
	}
}

func Debug(format string, v ...interface{}) {
	if enabled(DEBUG) {
		mu.Lock()
		logger.Print(formatLog(DEBUG, format, v...))
		mu.Unlock()
	}
}

type RequestLogger struct {
	ID        string
	Method    string
	Path      string
	Query     string
	IP        string
	StartTime time.Time
	mu        sync.RWMutex
}

func NewRequestLogger(method, path, query, ip string) *RequestLogger {
	return &RequestLogger{
		ID:        GenerateID()[:8],
		Method:    method,
		Path:      path,
		Query:     query,
		IP:        ip,
		StartTime: time.Now(),
	}
}

func (rl *RequestLogger) Start() {
	Info("[%s] %s %s %s", rl.ID, rl.Method, rl.Path, rl.Query)
}

func (rl *RequestLogger) End(statusCode int, format string, v ...interface{}) {
	rl.mu.Lock()
	duration := time.Since(rl.StartTime)
	rl.mu.Unlock()

	statusColor := "\033[32m"
	if statusCode >= 400 {
		statusColor = "\033[31m"
	} else if statusCode >= 300 {
		statusColor = "\033[33m"
	}

	if noColor {
		Info("[%s] %d %s (%s)", rl.ID, statusCode, fmt.Sprintf(format, v...), duration)
	} else {
		Info("%s[%d]%s %s (%s)", statusColor, statusCode, reset, fmt.Sprintf(format, v...), duration)
	}
}

func (rl *RequestLogger) Error(err error) {
	rl.mu.Lock()
	duration := time.Since(rl.StartTime)
	rl.mu.Unlock()

	Error("[%s] %s %s failed after %s: %v", rl.ID, rl.Method, rl.Path, duration, err)
}

func LogProviderRequest(provider, model string) {
	Debug("→ %s | model=%s", provider, model)
}

func LogProviderResponse(provider string, duration time.Duration, contentLen int) {
	Debug("← %s | duration=%s chars=%d", provider, duration, contentLen)
}

func LogServerStart(port string) {
	Info("%s v%s starting on :%s", brand.Name, brand.Version, port)
	Info("%s", "="+strings.Repeat("=", 50))
}

func LogProviders(providers map[string]map[string]interface{}) {
	Info("📦 Registered providers:")
	for name, info := range providers {
		Info("   • %s (%s)", name, info["label"])
	}
}

func LogEndpoints(port string) {
	Info("%s", "="+strings.Repeat("=", 50))
	Info("📍 Available endpoints:")
	Info("   POST  /v1/chat/completions")
	Info("   GET   /v1/models")
	Info("   GET   /v1/providers")
	Info("   GET   /health")
	Info("%s", "="+strings.Repeat("=", 50))
}

func LogBanner() {
	banner := brand.Banner()
	if !noColor {
		banner = "\033[37m" + banner + "\033[0m"
	}
	logger.Print(banner)
}
