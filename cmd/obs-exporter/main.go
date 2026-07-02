// Command obs-exporter is a Prometheus multi-target exporter for Dell
// ObjectScale 4.x / ECS 3.8.x clusters. See docs/design.md for the overall
// design and API research behind it.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promslog"
	promslogflag "github.com/prometheus/common/promslog/flag"
	commonversion "github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"

	"github.com/miyamadoKL/prometheus-obs-exporter/internal/collector"
	"github.com/miyamadoKL/prometheus-obs-exporter/internal/config"
	"github.com/miyamadoKL/prometheus-obs-exporter/internal/obsclient"
)

func main() {
	app := kingpin.New("obs-exporter", "Prometheus exporter for Dell ObjectScale 4.x / ECS 3.8.x clusters.")
	app.HelpFlag.Short('h')

	promslogConfig := &promslog.Config{}
	promslogflag.AddFlags(app, promslogConfig)

	cfg := config.New(app)

	app.Version(commonversion.Print("obs-exporter"))

	if _, err := app.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "obs-exporter: %v\n", err)
		os.Exit(2)
	}

	logger := promslog.New(promslogConfig)

	logger.Info("starting obs-exporter", "version", commonversion.Info(), "build_context", commonversion.BuildContext())

	prometheus.MustRegister(version.NewCollector("obs_exporter"))

	var clients sync.Map // target (string) -> *obsclient.Client

	landingPage, err := web.NewLandingPage(web.LandingConfig{
		Name:        "Dell ObjectScale / ECS Exporter",
		Description: "Prometheus exporter for Dell ObjectScale 4.x / ECS 3.8.x clusters",
		Version:     commonversion.Info(),
		Links: []web.LandingLinks{
			{Address: "/probe?target=<host>", Text: "Probe a target"},
			{Address: "/metrics", Text: "Exporter metrics"},
		},
	})
	if err != nil {
		logger.Error("error creating landing page", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.Handle("/", landingPage)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/probe", probeHandler(logger, cfg, &clients))

	srv := &http.Server{Handler: mux}

	// Trap SIGTERM/SIGINT: stop accepting new requests and drain active
	// ones (srv.Shutdown) before logging out of every cached target
	// (ECS/ObjectScale caps active tokens at 100 per user). Logging out
	// first would risk invalidating tokens still in use by in-flight
	// /probe requests; draining first ensures no request is scraping with
	// a client whose token gets pulled out from under it.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig.String())

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("error during HTTP server shutdown", "err", err)
		}

		logoutAll(&clients, logger)
	}()

	logger.Info("listening", "address", *cfg.Web.WebListenAddresses)
	if err := web.ListenAndServe(srv, &cfg.Web, logger); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("error running HTTP server", "err", err)
		os.Exit(1)
	}
}

// probeHandler implements GET /probe?target=<host>[&collectors=a,b,c].
func probeHandler(logger *slog.Logger, cfg *config.Config, clients *sync.Map) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		target := r.URL.Query().Get("target")
		if target == "" {
			http.Error(w, "target parameter is required", http.StatusBadRequest)
			return
		}

		collectors := collector.DefaultCollectors
		if raw := r.URL.Query().Get("collectors"); raw != "" {
			collectors = strings.Split(raw, ",")
		}

		client, err := getOrCreateClient(ctx, target, cfg, clients)
		if err != nil {
			logger.Error("could not obtain authenticated client for target", "target", target, "err", err)
			http.Error(w, fmt.Sprintf("could not connect to target %q: %v", target, err), http.StatusBadGateway)
			return
		}

		settings := collector.Settings{
			DTStatsEnabled:      cfg.CollectorNodeDTStats,
			MeteringConcurrency: cfg.CollectorMeteringConcurrency,
			PerfRange:           cfg.CollectorPerfRange,
			Logger:              logger,
		}

		registry := prometheus.NewRegistry()
		if err := collector.Probe(ctx, client, settings, collectors, registry); err != nil {
			logger.Error("probe failed", "target", target, "err", err)
			http.Error(w, fmt.Sprintf("probe of target %q failed: %v", target, err), http.StatusInternalServerError)
			return
		}

		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	}
}

// getOrCreateClient returns a cached, logged-in obsclient.Client for
// target, creating and logging in a new one if needed. Concurrent scrapes
// of the same, not-yet-cached target are resolved via sync.Map's
// LoadOrStore: whichever client loses the race logs itself back out
// immediately rather than leaking an extra token.
func getOrCreateClient(ctx context.Context, target string, cfg *config.Config, clients *sync.Map) (*obsclient.Client, error) {
	if v, ok := clients.Load(target); ok {
		return v.(*obsclient.Client), nil
	}

	c, err := obsclient.New(target, obsclient.Config{
		Username:              cfg.Username,
		Password:              cfg.Password,
		MgmtPort:              cfg.MgmtPort,
		ObjPort:               cfg.ObjPort,
		TLSInsecureSkipVerify: cfg.TLSInsecureSkipVerify,
		TLSCAFile:             cfg.TLSCAFile,
	})
	if err != nil {
		return nil, err
	}

	if err := c.Login(ctx); err != nil {
		return nil, err
	}

	actual, loaded := clients.LoadOrStore(target, c)
	if loaded {
		_ = c.Logout(ctx)
		return actual.(*obsclient.Client), nil
	}
	return c, nil
}

// logoutAll logs out of every cached client. Best-effort: failures are
// logged but do not prevent shutdown from proceeding.
func logoutAll(clients *sync.Map, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clients.Range(func(key, value any) bool {
		target, _ := key.(string)
		c, _ := value.(*obsclient.Client)
		if c == nil {
			return true
		}
		if err := c.Logout(ctx); err != nil {
			logger.Warn("failed to log out of target", "target", target, "err", err)
		}
		return true
	})
}
