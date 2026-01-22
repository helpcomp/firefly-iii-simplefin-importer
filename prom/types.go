package prom

import (
	"github.com/helpcomp/firefly-iii-simplefin-importer/config"
	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/helpcomp/firefly-iii-simplefin-importer/simplefin"
	"github.com/prometheus/client_golang/prometheus"
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
	ff                    *firefly.Firefly
	SimpleFinAccounts     []simplefin.Accounts
	config                *config.MasterConfig
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

func NewExporter(namespace string, newFireFly *firefly.Firefly, config *config.MasterConfig, accounts []simplefin.Accounts) *Exporter {
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
			namespace,
			"balance",
			"Balance for the given account",
		),
		AccountRefreshTime: prometheusAccountStatsDesc(
			namespace,
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
			namespace,
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
		ff:                newFireFly,
		config:            config,
		SimpleFinAccounts: accounts,
	}
}

func prometheusAccountStatsDesc(namespace string, metric string, help string) *prometheus.Desc {
	return prometheus.NewDesc(
		prometheus.BuildFQName(
			namespace,
			"account",
			metric,
		),
		help,
		[]string{"account_id", "account_name", "type"},
		nil,
	)
}

func prometheusFireflyStatsDesc(namespace string, metric string, help string) *prometheus.Desc {
	return prometheus.NewDesc(
		prometheus.BuildFQName(
			namespace,
			"status",
			metric,
		),
		help,
		[]string{},
		nil,
	)
}
