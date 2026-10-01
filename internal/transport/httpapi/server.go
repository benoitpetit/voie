package httpapi

import (
	"net"
	"net/http"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
)

func NewServer(service *app.Service, cfg *config.Config) *http.Server {
	return &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, cfg.Port),
		Handler:           NewHandler(service, cfg),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
