// Package config は、docs/design.md に基づき obs-exporter バイナリが
// 受け付けるコマンドラインフラグと環境変数を定義する。
package config

import (
	"fmt"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/exporter-toolkit/web"
)

// maxPerfRange は Flux API で規定されているレンジ上限（docs/design.md:
// "range 最大 1h 制約"）。--collector.perf.range にこれを超える値が設定された
// 場合、perf コレクターの毎スクレイプで失敗させるのではなく、起動時に拒否する。
const maxPerfRange = time.Hour

// EnvPrefix は、kingpin のフラグに加えてこの exporter が受け付ける
// 全環境変数に共通のプレフィックス。
const EnvPrefix = "OBS_EXPORTER_"

// Config holds all settings modifiable by the operator.
type Config struct {
	// ECS / ObjectScale への接続設定。
	Username string
	Password string
	MgmtPort int
	ObjPort  int

	TLSInsecureSkipVerify bool
	TLSCAFile             string

	// コレクターの挙動。
	CollectorNodeDTStats         bool
	CollectorMeteringConcurrency int
	CollectorPerfRange           time.Duration

	// web.go / exporter-toolkit のリッスン設定。
	Web web.FlagConfig
}

// New は指定された kingpin アプリケーションに全フラグを登録し、
// app.Parse() 呼び出し後に値が設定される Config を返す。
func New(app *kingpin.Application) *Config {
	c := &Config{}

	webListenAddresses := app.Flag(
		"web.listen-address",
		"Addresses on which to expose metrics and web interface.",
	).Default(":9438").Envar(EnvPrefix + "WEB_LISTEN_ADDRESS").Strings()

	webConfigFile := app.Flag(
		"web.config.file",
		"Path to configuration file that can enable TLS or authentication on the exporter's own listener.",
	).Default("").String()

	c.Web = web.FlagConfig{
		WebListenAddresses: webListenAddresses,
		WebConfigFile:      webConfigFile,
	}

	app.Flag(
		"ecs.username",
		"Username used to authenticate against ECS / ObjectScale (System Monitor role recommended).",
	).Envar(EnvPrefix + "USERNAME").Required().StringVar(&c.Username)

	app.Flag(
		"ecs.password",
		"Password used to authenticate against ECS / ObjectScale.",
	).Envar(EnvPrefix + "PASSWORD").Required().StringVar(&c.Password)

	app.Flag(
		"ecs.mgmt-port",
		"TCP port of the ECS / ObjectScale management API. Also used for the metering API.",
	).Default("4443").Envar(EnvPrefix + "MGMT_PORT").IntVar(&c.MgmtPort)

	app.Flag(
		"ecs.obj-port",
		"TCP port of the ECS / ObjectScale object (S3) API, used for the node ping check.",
	).Default("9021").Envar(EnvPrefix + "OBJ_PORT").IntVar(&c.ObjPort)

	app.Flag(
		"ecs.tls.insecure-skip-verify",
		"Skip TLS certificate verification when talking to ECS / ObjectScale. Opt-in only.",
	).Default("false").Envar(EnvPrefix + "TLS_INSECURE_SKIP_VERIFY").BoolVar(&c.TLSInsecureSkipVerify)

	app.Flag(
		"ecs.tls.ca-file",
		"Path to a PEM encoded CA certificate bundle used to verify ECS / ObjectScale's TLS certificate (for self-signed certificates).",
	).Default("").Envar(EnvPrefix + "TLS_CA_FILE").StringVar(&c.TLSCAFile)

	app.Flag(
		"collector.node.dt-stats",
		"Enable the DT statistics / ping node collector (:9101 and object port). Disable if the endpoint no longer exists on your platform.",
	).Default("true").Envar(EnvPrefix + "COLLECTOR_NODE_DT_STATS").BoolVar(&c.CollectorNodeDTStats)

	app.Flag(
		"collector.metering.concurrency",
		"Maximum number of concurrent requests issued against the metering API.",
	).Default("4").Envar(EnvPrefix + "METERING_CONCURRENCY").IntVar(&c.CollectorMeteringConcurrency)

	app.Flag(
		"collector.perf.range",
		"Flux query range for the perf collector (maximum 1h per the ECS/ObjectScale Flux API).",
	).Default("5m").Envar(EnvPrefix + "PERF_RANGE").DurationVar(&c.CollectorPerfRange)

	app.Validate(func(*kingpin.Application) error {
		if c.CollectorPerfRange > maxPerfRange {
			return fmt.Errorf("--collector.perf.range (%s) must not exceed %s (Flux API range limit)", c.CollectorPerfRange, maxPerfRange)
		}
		return nil
	})

	return c
}
