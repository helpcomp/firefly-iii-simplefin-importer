package main

import (
	"github.com/alecthomas/kong"
	"github.com/helpcomp/firefly-iii-simplefin-importer/config"
	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/helpcomp/firefly-iii-simplefin-importer/prom"
	"github.com/helpcomp/firefly-iii-simplefin-importer/simplefin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sashabaranov/go-openai"
	"net/http"
	"os"
	"time"
)

var (
	ff *firefly.Firefly
	c  *config.MasterConfig

	oai  *openai.Client
	sfin *simplefin.Simplefin
)

var cli struct {
	MetricsPath                 string `env:"EXPORTER_METRICS_PATH" help:"${env} - Path under which to expose metrics" default:"/metrics"`
	ConfigPath                  string `env:"CONFIG_PATH" help:"${env} - Path to config file" default:"./config.yml"`
	ListenAddress               string `env:"EXPORTER_LISTEN_ADDRESS" help:"${env} - Address to listen on for web interface and telemetry" default:"9717"`
	FireflyToken                string `env:"FIREFLY_TOKEN" help:"${env} - Firefly Token" required:""`
	FireflyBase                 string `env:"FIREFLY_URL" help:"${env} - Firefly URL" required:""`
	SimplefinAccessURL          string `env:"SIMPLEFIN_ACCESS_URL" help:"${env} - Simplefin Access URL" required:""`
	SimplefinLoopbackDuration   string `env:"SIMPLEFIN_LOOPBACK_DURATION" help:"${env} - How far back should Simplefin pull transaction data" default:"10d"`
	OpenAIAPIKey                string `env:"OPENAI_API_KEY" help:"${env} - API Key for OpenAI. If none is provided, OpenAI support is disabled"`
	RefreshTime                 uint16 `env:"REFRESH_TIME" help:"${env} - Time in minutes for refresh (Default 1440 / 1 day)" default:"1440"`
	EnablePrometheus            bool   `env:"ENABLE_PROMETHEUS" help:"${env} - Enable Prometheus metrics" default:"true"`
	FireflyEnableReconciliation bool   `env:"ENABLE_AUTO_RECONCILIATION" help:"${env} - Enables Automatic Reconciliation of the accounts" default:"false"`
	CacheOnly                   bool   `env:"DEBUG_CACHE_ONLY" help:"${env} - Cache Only - Does not query Simplefin unless no cache exists (Debug)" default:"false"`
	DoNotUpdateTransactions     bool   `env:"DEBUG_DO_NOT_UPDATE_TRANSACTIONS" help:"${env} - Do not update / post new transactions (Debug)" default:"false"`
}

func main() {
	// SETUP //
	//////////
	kong.Parse(&cli)                                                                             // Kong Parser
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout})                               // Logger
	c = config.InitConfig(cli.ConfigPath)                                                        // Config
	ff = firefly.New(&http.Client{Timeout: time.Second * 30}, cli.FireflyToken, cli.FireflyBase) // Firefly
	sfin = simplefin.New(cli.SimplefinAccessURL, cli.CacheOnly)                                  // SimpleFIN
	if cli.OpenAIAPIKey != "" {
		oai = openai.NewClient(cli.OpenAIAPIKey)
	} // OpenAI

	// Refresher
	ticker := time.NewTicker(time.Duration(cli.RefreshTime) * time.Minute)
	quit := make(chan struct{})

	startUpdate()

	// No Prometheus Support, refresh only
	if !cli.EnablePrometheus {
		log.Info().Msg("Prometheus metrics are disabled. Refresh only.")
		for {
			select {
			case <-ticker.C:
				startUpdate()
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}

	// Prometheus Support. Refresh and Metrics
	go func() {
		for {
			select {
			case <-ticker.C:
				startUpdate()
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	prometheus.MustRegister(prom.NewExporter(ff, cli.ConfigPath))
	http.Handle(cli.MetricsPath, promhttp.Handler())
	if cli.MetricsPath != "/" && cli.MetricsPath != "" {
		landingConfig := web.LandingConfig{
			Name:        "FireFly III Exporter",
			Description: "Prometheus Firefly III Exporter",
			Version:     version.Print("firefly-iii-simplefin-importer"),
			Links: []web.LandingLinks{
				{
					Address: cli.MetricsPath,
					Text:    "Metrics",
				},
				{
					Address: "/health",
					Text:    "Health",
				},
			},
		}
		landingPage, err := web.NewLandingPage(landingConfig)

		if err != nil {
			log.Fatal().Err(err).Msg("")
		}
		http.Handle("/", landingPage)
		http.HandleFunc("/health", prom.HealthHandler)
	}

	log.Info().Msgf("Starting HTTP server on listen address :%s and metric path %s", cli.ListenAddress, cli.MetricsPath)

	if err := http.ListenAndServe(":"+cli.ListenAddress, nil); err != nil {
		log.Fatal().Err(err).Msg("")
	}
}
