package prom

import (
	"sync"

	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/shopspring/decimal"
)

const maxConcurrentWorkers = 10

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.CollectAccounts(ch) // Firefly Account Collector
	e.CollectSys(ch)      // Program Collector (API calls, etc...)
}

func (e *Exporter) CollectAccounts(ch chan<- prometheus.Metric) {
	var wg sync.WaitGroup                           // Used for goroutines - Wait for multiple goroutines to finish
	cachedAccounts, _ := e.ff.ListAccounts("asset") // Get Accounts
	cats, _ := e.ff.CachedCategories()

	// Create job channels
	type accountJob struct {
		account firefly.Account
	}
	type categoryJob struct {
		category firefly.Category
	}

	accountJobs := make(chan accountJob, len(cachedAccounts))
	categoryJobs := make(chan categoryJob, len(cats))

	// Start a worker pool for accounts
	for i := 0; i < maxConcurrentWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range accountJobs {
				e.collectAccountTransactions(job.account.ID, job.account.Attributes.Name, job.account.Attributes.Type, ch)
			}
		}()
	}

	// Accounts //
	//////////////
	// Send account jobs to workers
	for _, account := range cachedAccounts {
		accountJobs <- accountJob{account: account}

		// Account Balance (send directly, no need to go through worker)
		ch <- prometheus.MustNewConstMetric(
			e.AccountBalance,
			prometheus.GaugeValue,
			account.Attributes.CurrentBalance.InexactFloat64(),
			account.ID, account.Attributes.Name, account.Attributes.Type,
		)

		// Account Balance Date
		for _, acct := range e.SimpleFinAccounts {
			if e.config.Accounts[acct.ID] == account.ID {
				ch <- prometheus.MustNewConstMetric(
					e.AccountRefreshTime,
					prometheus.GaugeValue,
					float64(acct.BalanceDate),
					account.ID, account.Attributes.Name, account.Attributes.Type,
				)
				break
			}
		}
	}
	close(accountJobs) // Signal no more account jobs

	// Wait for account workers to finish
	wg.Wait()

	// Category //
	/////////////
	// Start worker pool for categories
	for i := 0; i < maxConcurrentWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range categoryJobs {
				e.collectCategoryTransactions(job.category.ID, job.category.Name, ch)
			}
		}()
	}

	// Send category jobs to workers
	for _, cat := range cats {
		categoryJobs <- categoryJob{category: cat}
	}
	close(categoryJobs) // Signal no more category jobs

	// Wait for category workers to finish
	wg.Wait()
}

// collectAccountTransactions Scrapes Firefly for Account Transactions based on the given account ID
func (e *Exporter) collectAccountTransactions(id string, name string, acctType string, ch chan<- prometheus.Metric) {
	e.txnCache.mu.RLock()
	cachedCount, exists := e.txnCache.counts[id]
	e.txnCache.mu.RUnlock()

	if !exists {
		accountTrans, err := e.ff.ListAccountTransactions(id)
		if err != nil {
			return
		}
		e.txnCache.mu.Lock()
		e.txnCache.counts[id] = accountTrans.Meta.Pagination.Total
		e.txnCache.mu.Unlock()
		cachedCount = accountTrans.Meta.Pagination.Total
	}

	ch <- prometheus.MustNewConstMetric(
		e.AccountTransactions,
		prometheus.CounterValue,
		float64(cachedCount),
		id, name, acctType,
	)
}

// collectCategoryTransactions Scrapes Firefly for Category Transactions based on the given category ID
func (e *Exporter) collectCategoryTransactions(id int, name string, ch chan<- prometheus.Metric) {
	e.catCache.mu.RLock()
	cachedCount, exists := e.catCache.counts[id]
	cachedBalance := e.catCache.balances[id]
	e.catCache.mu.RUnlock()

	if !exists {
		categoryTrans, err := e.ff.ListCategoryTransactions(id)
		if err != nil {
			return
		}
		catAmt := decimal.Decimal{}
		for _, categoryTransP := range categoryTrans.Data {
			for _, catTransC := range categoryTransP.Attributes.Transactions {
				catAmt = catAmt.Add(catTransC.Amount)
			}
		}

		e.catCache.mu.Lock()
		e.catCache.counts[id] = categoryTrans.Meta.Pagination.Total
		e.catCache.balances[id] = catAmt
		e.catCache.mu.Unlock()

		cachedCount = categoryTrans.Meta.Pagination.Total
		cachedBalance = catAmt
	}

	ch <- prometheus.MustNewConstMetric(
		e.categoryActivity,
		prometheus.GaugeValue,
		float64(cachedCount),
		name,
	)
	ch <- prometheus.MustNewConstMetric(
		e.categoryBalance,
		prometheus.GaugeValue,
		cachedBalance.InexactFloat64(),
		name,
	)
}

// CollectSys Collects Program information (API calls, etc...)
func (e *Exporter) CollectSys(ch chan<- prometheus.Metric) {
	/*ch <- prometheus.MustNewConstMetric(
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
	)*/
}
