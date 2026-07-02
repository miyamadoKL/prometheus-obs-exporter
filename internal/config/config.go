// Package config defines the command-line flags and environment variables
// accepted by the obs-exporter binary, following docs/design.md.
package config

import (
	"fmt"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/exporter-toolkit/web"
)

// maxPerfRange is the Flux API's documented range cap (docs/design.md:
// "range 最大 1h 制約"). A configured --collector.perf.range beyond this is
// rejected at startup rather than failing every perf-collector scrape at
// request time.
const maxPerfRange = time.Hour

// EnvPrefix is the prefix used for all environment variables accepted by
// the exporter (in addition to the kingpin flags themselves).
const EnvPrefix = "OBS_EXPORTER_"

// Config holds all settings modifiable by the operator.
type Config struct {
	// ECS / ObjectScale connection settings.
	Username string
	Password string
	MgmtPort int
	ObjPort  int

	TLSInsecureSkipVerify bool
	TLSCAFile             string

	// Collector behavior.
	CollectorNodeDTStats         bool
	CollectorMeteringConcurrency int
	CollectorPerfRange           time.Duration

	// web.go / exporter-toolkit listen configuration.
	Web web.FlagConfig
}

// New registers all flags on the given kingpin application and returns the
// Config that will be populated once app.Parse() is called.
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
