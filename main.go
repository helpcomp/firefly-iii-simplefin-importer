package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "net/http/pprof"

	"github.com/alecthomas/kong"
	"github.com/helpcomp/firefly-iii-simplefin-importer/config"
	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/helpcomp/firefly-iii-simplefin-importer/prom"
	"github.com/helpcomp/firefly-iii-simplefin-importer/simplefin"
	"github.com/prometheus/client_golang/prometheus"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/rs/zerolog/log"
	"github.com/sashabaranov/go-openai"
)

const AppName = "firefly-iii-simplefin-importer"
const AppDesc = "Go-based service that connects your SimpleFIN-enabled financial institutions to Firefly III. It periodically fetches account data and transactions from SimpleFIN, syncs them into Firefly III."

var cli struct {
	MetricsPath                 string `env:"EXPORTER_METRICS_PATH" help:"${env} - Path under which to expose metrics" default:"/metrics"`
	ConfigPath                  string `env:"CONFIG_PATH" help:"${env} - Path to config file" default:"./config.yml"`
	ListenAddress               string `env:"EXPORTER_LISTEN_ADDRESS" help:"${env} - Address to listen on for web interface and telemetry" default:"9717"`
	FireflyToken                string `env:"FIREFLY_TOKEN" help:"${env} - Firefly Token" required:""`
	FireflyBase                 string `env:"FIREFLY_URL" help:"${env} - Firefly URL" required:""`
	SimplefinAccessURL          string `env:"SIMPLEFIN_ACCESS_URL" help:"${env} - Simplefin Access URL" required:""`
	SimplefinLoopbackDuration   string `env:"SIMPLEFIN_LOOPBACK_DURATION" help:"${env} - How far back should Simplefin pull transaction data" default:"10d"`
	AzureAIAPIKey               string `env:"AZURE_API_KEY" help:"${env} - API Key for Azure OpenAI. If none is provided, OpenAI support is disabled"`
	AzureEndpoint               string `env:"AZURE_ENDPOINT" help:"${env} - Azure OpenAI Endpoint"`
	OpenAIAPIKey                string `env:"OPENAI_API_KEY" help:"${env} - API Key for OpenAI. If none is provided, OpenAI support is disabled"`
	OpenAIModel                 string `env:"OPENAI_MODEL" help:"${env} - OpenAI Model type. Default is gpt-3.5-turbo-instruct" default:"gpt-3.5-turbo-instruct"`
	RefreshTime                 uint16 `env:"REFRESH_TIME" help:"${env} - Time in minutes for refresh (Default 1440 / 1 day)" default:"1440"`
	EnablePrometheus            bool   `env:"ENABLE_PROMETHEUS" help:"${env} - Enable Prometheus metrics" default:"true"`
	FireflyEnableReconciliation bool   `env:"ENABLE_AUTO_RECONCILIATION" help:"${env} - Enables Automatic Reconciliation of the accounts" default:"false"`
	AutoRemoveTransactions      bool   `env:"ENABLE_AUTO_TRANSACTION_REMOVAL" help:"${env} - Removes transactions that no longer exist in SimpleFIN" default:"false"`
	CacheOnly                   bool   `env:"DEBUG_CACHE_ONLY" help:"${env} - Cache Only - Does not query Simplefin unless no cache exists (Debug)" default:"false"`
	DoNotUpdateTransactions     bool   `env:"DEBUG_DO_NOT_UPDATE_TRANSACTIONS" help:"${env} - Do not update / post new transactions (Debug)" default:"false"`
}

func main() {
	// Variable Setup //
	///////////////////
	kong.Parse(&cli,
		kong.Name(AppName),
		kong.Description(AppDesc),
	)
	log.Logger = log.Output(os.Stderr).With().Caller().Logger()                                   // Logger
	var oai *openai.Client                                                                        // OpenAI
	sf := simplefin.New(cli.SimplefinAccessURL, cli.CacheOnly)                                    // Simplefin
	ff := firefly.New(&http.Client{Timeout: time.Second * 30}, cli.FireflyToken, cli.FireflyBase) // Firefly
	cfg := config.InitConfig(cli.ConfigPath)                                                      // Config
	var simplefinAccounts []simplefin.Accounts

	// AI Setup //
	/////////////
	// OpenAI
	if cli.OpenAIAPIKey != "" {
		oai = openai.NewClient(cli.OpenAIAPIKey)
	}
	// AzureAI
	if cli.AzureAIAPIKey != "" {
		if cli.AzureEndpoint == "" {
			log.Error().Msg("Azure Endpoint is required if Azure API Key is provided")
		} else {
			oai = openai.NewClientWithConfig(openai.DefaultAzureConfig(cli.AzureAIAPIKey, cli.AzureEndpoint))
		}
	}

	// Start //
	///////////
	log.Logger.Info().
		Str("version", version.Info()).
		Msg("Starting " + AppName)

	// Create a channel to listen for interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Refresher
	ticker := time.NewTicker(time.Duration(cli.RefreshTime) * time.Minute)
	quit := make(chan struct{})

	// Immediately start a refresh of the data in the background
	go func() {
		simplefinAccounts = startUpdate(sf, ff, cfg, oai)
	}()

	// No Prometheus Support, refresh only
	if !cli.EnablePrometheus {
		log.Info().Msg("Prometheus metrics are disabled. Refresh only.")
		for {
			select {
			case <-ticker.C:
				simplefinAccounts = startUpdate(sf, ff, cfg, oai)
			case <-quit:
				ticker.Stop()
				return
			case sig := <-sigChan:
				log.Info().Msgf("Received signal %s. Exiting...", sig)
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
				simplefinAccounts = startUpdate(sf, ff, cfg, oai)
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	// Metric Registration
	prometheus.MustRegister(
		versioncollector.NewCollector(AppName),
		prom.NewExporter(AppName, ff, cfg, simplefinAccounts),
	)

	// HTTP Server
	http.Handle(cli.MetricsPath, promhttp.Handler())
	if cli.MetricsPath != "/" && cli.MetricsPath != "" {
		landingConfig := web.LandingConfig{
			Name:        AppName,
			Description: AppDesc,
			Version:     version.Print(AppName),
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

	server := &http.Server{
		Addr:         ":" + cli.ListenAddress,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Listen and serve
	go func() {
		log.Printf("Server starting on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("Error starting HTTP server")
		}
	}()

	// Handle shutdown
	<-sigChan
	log.Info().Msg("Shutdown Signal Received")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log.Info().Msg("Shutting down HTTP server...")
	_ = server.Shutdown(ctx)
	log.Info().Msg("Stopping Metric Refresh ticker")
	ticker.Stop()
	log.Info().Msg("Shutdown Complete; Exiting...")
}
