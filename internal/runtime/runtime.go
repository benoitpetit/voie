package runtime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/benoitpetit/voie/config"
	"github.com/benoitpetit/voie/internal/app"
	"github.com/benoitpetit/voie/internal/brand"
	sqlitestore "github.com/benoitpetit/voie/internal/storage/sqlite"
	"github.com/benoitpetit/voie/internal/transport/httpapi"
	"github.com/benoitpetit/voie/internal/transport/mcp"
	"github.com/benoitpetit/voie/providers"
	"github.com/benoitpetit/voie/utils"
)

type Runtime struct {
	Config            *config.Config
	Registry          *app.Registry
	AppService        *app.Service
	ConversationStore *sqlitestore.Store
}

func New(cfg *config.Config) (*Runtime, error) {
	return NewWithRegistry(cfg, providers.NewRegistry())
}

func NewWithRegistry(cfg *config.Config, registry *app.Registry) (*Runtime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	if registry == nil {
		return nil, fmt.Errorf("provider registry is required")
	}
	routingPath := strings.TrimSpace(cfg.RoutingConfigPath)
	if routingPath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve user configuration directory: %w", err)
		}
		routingPath = filepath.Join(configDir, "voie", "routing.json")
	}
	policyConfig, err := config.LoadRoutingPolicy(routingPath)
	if err != nil {
		return nil, err
	}
	registeredModels := make(map[string]struct{})
	for _, name := range registry.GetProviderNames() {
		for _, model := range registry.Get(name).GetInfo().SupportedModels {
			registeredModels[strings.ToLower(strings.TrimSpace(model))] = struct{}{}
		}
	}
	if err := config.ValidateRoutingPolicy(policyConfig, registeredModels); err != nil {
		return nil, err
	}
	policy := app.RoutingPolicy{Models: make(map[string]app.ModelDescriptor, len(policyConfig.Models)), Tasks: make(map[string]app.TaskRule, len(policyConfig.Tasks))}
	for model, descriptor := range policyConfig.Models {
		policy.Models[model] = app.ModelDescriptor{Description: descriptor.Description, Capabilities: append([]string(nil), descriptor.Capabilities...)}
	}
	for task, rule := range policyConfig.Tasks {
		policy.Tasks[task] = app.TaskRule{RequiredCapabilities: append([]string(nil), rule.RequiredCapabilities...), PreferredModels: append([]string(nil), rule.PreferredModels...)}
	}
	fallback := app.FallbackPolicy{Enabled: true, MaxRetries: 1, MaxFallbackModels: 1}
	if cfg.FallbackEnabled != nil {
		fallback.Enabled = *cfg.FallbackEnabled
	}
	if cfg.FallbackMaxRetries != nil {
		fallback.MaxRetries = *cfg.FallbackMaxRetries
	}
	if cfg.FallbackMaxModels != nil {
		fallback.MaxFallbackModels = *cfg.FallbackMaxModels
	}
	if policyConfig.Fallback != nil {
		if policyConfig.Fallback.Enabled != nil {
			fallback.Enabled = *policyConfig.Fallback.Enabled
		}
		if policyConfig.Fallback.MaxRetries != nil {
			fallback.MaxRetries = *policyConfig.Fallback.MaxRetries
		}
		if policyConfig.Fallback.MaxFallbackModels != nil {
			fallback.MaxFallbackModels = *policyConfig.Fallback.MaxFallbackModels
		}
		if policyConfig.Fallback.Models != nil {
			fallback.Models = make(map[string][]string, len(policyConfig.Fallback.Models))
			for model, candidates := range policyConfig.Fallback.Models {
				fallback.Models[model] = append([]string(nil), candidates...)
			}
		}
	}
	conversationPath := strings.TrimSpace(cfg.ConversationDBPath)
	if conversationPath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve conversation database directory: %w", err)
		}
		conversationPath = filepath.Join(configDir, "voie", "conversations.db")
	}
	conversationStore, err := sqlitestore.NewSQLiteStore(conversationPath, cfg.ConversationTTL)
	if err != nil {
		return nil, err
	}
	service, err := app.NewService(registry, app.ServiceOptions{
		DefaultProvider: cfg.DefaultProvider,
		RouterModel:     cfg.RouterModel,
		SynthesisModel:  cfg.SynthesisModel,
		RoutingPolicy:   policy,
		Fallback:        &fallback,
		Conversations:   conversationStore,
		Timeout:         cfg.Timeout,
	})
	if err != nil {
		return nil, err
	}
	return &Runtime{Config: cfg, Registry: registry, AppService: service, ConversationStore: conversationStore}, nil
}

func (r *Runtime) Address() string {
	return net.JoinHostPort(r.Config.Host, r.Config.Port)
}

func (r *Runtime) Service() *app.Service { return r.AppService }

func (r *Runtime) Serve(ctx context.Context, host, port string) error {
	defer r.Close()
	if host == "" {
		host = r.Config.Host
	}
	if port == "" {
		port = r.Config.Port
	}
	cfg := *r.Config
	cfg.Host, cfg.Port = host, port
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid bind configuration: %w", err)
	}
	utils.Info("Starting %s %s HTTP server on %s", brand.Name, brand.Version, net.JoinHostPort(cfg.Host, cfg.Port))
	utils.Info("HTTP routes: /, /health, /v1/chat/completions, /v1/models, /v1/providers")
	utils.Info("Provider catalogue: %d providers, %d models", len(r.AppService.ListProviderMetadata()), len(r.AppService.ListModels()))
	server := httpapi.NewServer(r.AppService, &cfg)
	serverErrors := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		} else {
			serverErrors <- nil
		}
	}()
	select {
	case err := <-serverErrors:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	}
}

func (r *Runtime) MCP(ctx context.Context) error {
	defer r.Close()
	return mcpserver.Run(ctx, r.AppService)
}

func (r *Runtime) Close() error {
	if r.ConversationStore != nil {
		return r.ConversationStore.Close()
	}
	return nil
}
