package prom

import (
	"fmt"
	"github.com/helpcomp/firefly-iii-simplefin-importer/config"
	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/helpcomp/firefly-iii-simplefin-importer/simplefin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sashabaranov/go-openai"
)

type APIStruct struct {
	Firefly   float64
	Simplefin float64
	OpenAI    float64
}

var (
	namespace = "firefly"
	ff        *firefly.Firefly

	Sfinresp simplefin.AccountsResponse

	Oai_usage openai.Usage

	Oai_response_failure float64

	mconfig *config.MasterConfig

	APIErrors     APIStruct
	APICalls      APIStruct
	ProgramErrors float64 = 0
)

type Exporter struct {
	AccountTransactions   *prometheus.Desc
	AccountBalance        *prometheus.Desc
	AccountRefreshTime    *prometheus.Desc
	OpenAITokens          *prometheus.Desc
	OpenAIResponseFailure *prometheus.Desc
	APICalls              *prometheus.Desc
	APIErrors             *prometheus.Desc
	ProgramErrors         *prometheus.Desc
	RefreshTime           *prometheus.Desc
	RateLimit             *prometheus.Desc
	categoryActivity      *prometheus.Desc
	categoryBalance       *prometheus.Desc
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.AccountTransactions
	ch <- e.AccountBalance
	ch <- e.AccountRefreshTime
	ch <- e.APICalls
	ch <- e.APIErrors
	ch <- e.ProgramErrors
	ch <- e.OpenAITokens
	ch <- e.OpenAIResponseFailure
	ch <- e.categoryActivity
	ch <- e.categoryBalance
}

func NewExporter(newFireFly *firefly.Firefly) *Exporter {
	ff = newFireFly
	mconfig = config.InitConfig()
	return &Exporter{
		AccountTransactions: prometheus.NewDesc(
			prometheus.BuildFQName(
				namespace,
				"account",
				"transactions",
			),
			"How many transactions for the given account",
			[]string{"account_id", "account_name", "type"},
			nil,
		),
		AccountBalance: prometheusAccountStatsDesc(
			"balance",
			"Balance for the given account",
		),
		AccountRefreshTime: prometheusAccountStatsDesc(
			"refresh_time",
			"Time account was last refreshed (Unix Time / Epoch)",
		),
		OpenAIResponseFailure: prometheus.NewDesc(
			prometheus.BuildFQName(
				namespace,
				"openai",
				"response_failure",
			),
			"Has OpenAI failed",
			[]string{},
			nil,
		),
		OpenAITokens: prometheus.NewDesc(
			prometheus.BuildFQName(
				namespace,
				"openai",
				"tokens",
			),
			"Count of OpenAI Tokens",
			[]string{"type"},
			nil,
		),
		APICalls: prometheus.NewDesc(
			prometheus.BuildFQName(
				namespace,
				"status",
				"api_calls",
			),
			"Count of API calls",
			[]string{"type"},
			nil,
		),
		APIErrors: prometheus.NewDesc(
			prometheus.BuildFQName(
				namespace,
				"status",
				"api_errors",
			),
			"Count of API Errors",
			[]string{"type"},
			nil,
		),
		ProgramErrors: prometheusFireflyStatsDesc(
			"program_errors",
			"Current status of the system",
		),
		categoryActivity: prometheus.NewDesc(
			prometheus.BuildFQName(
				namespace,
				"category",
				"activity",
			),
			"Count of category transactions",
			[]string{"type"},
			nil,
		),
		categoryBalance: prometheus.NewDesc(
			prometheus.BuildFQName(
				namespace,
				"category",
				"balance",
			),
			"Balance of Category",
			[]string{"type"},
			nil,
		),
	}
}

func prometheusAccountStatsDesc(metric string, help string) *prometheus.Desc {
	return prometheus.NewDesc(
		prometheus.BuildFQName(
			namespace,
			"account",
			fmt.Sprintf("%s", metric),
		),
		help,
		[]string{"account_id", "account_name", "type"},
		nil,
	)
}

func prometheusFireflyStatsDesc(metric string, help string) *prometheus.Desc {
	return prometheus.NewDesc(
		prometheus.BuildFQName(
			namespace,
			"status",
			fmt.Sprintf("%s", metric),
		),
		help,
		[]string{},
		nil,
	)
}
