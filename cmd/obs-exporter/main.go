// Command obs-exporter は Dell ObjectScale 4.x / ECS 3.8.x クラスタ向けの
// Prometheus マルチターゲット exporter。全体設計と背景となる API 調査は
// docs/design.md を参照。
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

	// SIGTERM/SIGINT を捕捉し、新規リクエストの受付を止めて処理中のリクエストを
	// ドレイン（srv.Shutdown）してから、キャッシュ済みの全ターゲットをログアウトする
	// （ECS/ObjectScale はユーザーごとに有効トークン数の上限が100件）。
	// 先にログアウトすると、処理中の /probe リクエストが使っているトークンを
	// 無効化してしまう恐れがある。先にドレインすることで、トークンを
	// 足元から抜かれた状態でスクレイプ中のクライアントが発生しないようにする。
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

// probeHandler は GET /probe?target=<host>[&collectors=a,b,c] を実装する。
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

// getOrCreateClient は target に対応するキャッシュ済み・ログイン済みの
// obsclient.Client を返し、なければ新規作成してログインする。まだキャッシュ
// されていない同一 target への同時スクレイプは sync.Map の LoadOrStore で
// 解決される。競合に負けたクライアントは、余分なトークンをリークしないよう
// 即座に自分でログアウトする。
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

// logoutAll はキャッシュ済みの全クライアントをログアウトする。ベストエフォート
// であり、失敗してもログに残すのみでシャットダウン処理は継続する。
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
