package prom

import (
	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/helpcomp/firefly-iii-simplefin-importer/simplefin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/shopspring/decimal"
	"sync"
)

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.CollectAccounts(ch) // Firefly Account Collector
	e.CollectSys(ch)      // Program Collector (API calls, etc...)
}

func (e *Exporter) CollectAccounts(ch chan<- prometheus.Metric) {
	var wg sync.WaitGroup                         // Used for goroutines - Wait for multiple goroutines to finish
	cachedAccounts, _ := ff.ListAccounts("asset") // Get Accounts

	// Accounts //
	//////////////
	// Account Transactions
	for _, account := range cachedAccounts {
		wg.Add(1)
		go func(account firefly.Account) {
			// Decrement the counter when the goroutine completes.
			defer wg.Done()
			// Fetch the URL.
			e.collectAccountTransactions(account.ID, account.Attributes.Name, account.Attributes.Type, ch)
		}(account)

		// Account Balance
		ch <- prometheus.MustNewConstMetric(
			e.AccountBalance,
			prometheus.GaugeValue,
			account.Attributes.CurrentBalance.InexactFloat64(),
			account.ID, account.Attributes.Name, account.Attributes.Type,
		)

		// Account Balance Date
		for _, sfinAccount := range Sfinresp.Accounts {
			if mconfig.Accounts[sfinAccount.ID] == account.ID {
				ch <- prometheus.MustNewConstMetric(
					e.AccountRefreshTime,
					prometheus.GaugeValue,
					float64(sfinAccount.BalanceDate),
					account.ID, account.Attributes.Name, account.Attributes.Type,
				)
				break
			}
		}
	}
	// Category //
	/////////////
	cats, _ := ff.CachedCategories()
	// Category Transactions
	for _, cat := range cats {
		wg.Add(1)
		go func(cat firefly.Category) {
			defer wg.Done()
			e.collectCategoryTransactions(cat.ID, cat.Name, ch)
		}(cat)
	}
	wg.Wait() // Wait for all the goroutines to finish
}

// collectAccountTransactions Scrapes Firefly for Account Transactions based on the given account ID
func (e *Exporter) collectAccountTransactions(id string, name string, acctType string, ch chan<- prometheus.Metric) {
	accountTrans, _ := ff.ListAccountTransactions(id)

	ch <- prometheus.MustNewConstMetric(
		e.AccountTransactions,
		prometheus.CounterValue,
		float64(accountTrans.Meta.Pagination.Total),
		id, name, acctType,
	)
}

// collectCategoryTransactions Scrapes Firefly for Category Transactions based on the given category ID
func (e *Exporter) collectCategoryTransactions(id int, name string, ch chan<- prometheus.Metric) {
	categoryTrans, _ := ff.ListCategoryTransactions(id)
	CatAmt := decimal.Decimal{}

	for _, categoryTransP := range categoryTrans.Data {
		for _, catTransC := range categoryTransP.Attributes.Transactions {
			CatAmt = CatAmt.Add(catTransC.Amount)
		}
	}

	ch <- prometheus.MustNewConstMetric(
		e.categoryActivity,
		prometheus.GaugeValue,
		float64(categoryTrans.Meta.Pagination.Total),
		name,
	)
	ch <- prometheus.MustNewConstMetric(
		e.categoryBalance,
		prometheus.GaugeValue,
		CatAmt.InexactFloat64(),
		name,
	)
}

// CollectSys Collects Program information (API calls, etc...)
func (e *Exporter) CollectSys(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(
		e.APICalls,
		prometheus.CounterValue,
		APICalls.Firefly,
		"firefly",
	)
	ch <- prometheus.MustNewConstMetric(
		e.APICalls,
		prometheus.CounterValue,
		simplefin.APICalls,
		"simplefin",
	)
	ch <- prometheus.MustNewConstMetric(
		e.APICalls,
		prometheus.CounterValue,
		APICalls.OpenAI,
		"openai",
	)
	ch <- prometheus.MustNewConstMetric(
		e.OpenAITokens,
		prometheus.CounterValue,
		float64(OaiUsage.CompletionTokens),
		"completion",
	)
	ch <- prometheus.MustNewConstMetric(
		e.OpenAITokens,
		prometheus.CounterValue,
		float64(OaiUsage.TotalTokens),
		"total",
	)
	ch <- prometheus.MustNewConstMetric(
		e.OpenAITokens,
		prometheus.CounterValue,
		float64(OaiUsage.PromptTokens),
		"prompt",
	)
	ch <- prometheus.MustNewConstMetric(
		e.APIErrors,
		prometheus.CounterValue,
		APIErrors.Firefly,
		"firefly",
	)
	ch <- prometheus.MustNewConstMetric(
		e.APIErrors,
		prometheus.CounterValue,
		APIErrors.Simplefin,
		"simplefin",
	)
	ch <- prometheus.MustNewConstMetric(
		e.ProgramErrors,
		prometheus.CounterValue,
		ProgramErrors,
	)
}
