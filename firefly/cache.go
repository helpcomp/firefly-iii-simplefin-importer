package firefly

import (
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Since queries to firefly are slow (up to 5 seconds), keep a cache of these
// requests. Allow the cache to be initialized, and selectively updated
// on-demand. If we are only using this app to record new transactions, the
// cache should always be fresh.

type Cache struct {
	Accounts       []Account
	Categories     []Category
	CategoryTotals map[categoryTotalsKey][]CategoryTotal
	Transactions   map[TransactionsKey][]Transactions
	mu             sync.Mutex // Protects the cached data
}

type categoryTotalsKey struct {
	CategoryID int
	Start      time.Time
	End        time.Time
}

type TransactionsKey struct {
	Page  int
	Start string
	End   string
	Type  string
}

type AccountCache struct {
	Accounts       []Account
	AccountsByID   map[string]Account
	AccountsByName map[string]Account
}

func (f *Firefly) CachedAccounts() (AccountCache, error) {
	f.cache.mu.Lock()
	defer f.cache.mu.Unlock()

	if f.cache.Accounts == nil {
		err := f.refreshAccounts()
		if err != nil {
			return AccountCache{}, err
		}
	}
	return buildAccountIndexes(f.cache.Accounts), nil
}

func buildAccountIndexes(accounts []Account) AccountCache {
	cache := AccountCache{
		Accounts:       accounts,
		AccountsByID:   make(map[string]Account, len(accounts)),
		AccountsByName: make(map[string]Account, len(accounts)),
	}

	for _, acct := range accounts {
		cache.AccountsByID[acct.ID] = acct
		cache.AccountsByName[acct.Attributes.Name] = acct
	}

	return cache
}

func (f *Firefly) refreshAccounts() error {
	c, err := f.ListAccounts("")
	if err != nil {
		return err
	}
	log.Debug().Msg("Cache: updating Accounts")
	f.cache.Accounts = c
	return nil
}

func (f *Firefly) CachedCategories() ([]Category, error) {
	f.cache.mu.Lock()
	defer f.cache.mu.Unlock()
	if f.cache.Categories == nil {
		err := f.refreshCategories()
		if err != nil {
			return nil, err
		}
	}
	return f.cache.Categories, nil
}

// refreshCategories refreshes the cached Categories. The caller is responsible
// for locking the mutex.
func (f *Firefly) refreshCategories() error {
	c, err := f.Categories()
	if err != nil {
		return err
	}
	log.Debug().Msg("Cache: updating Categories")
	f.cache.Categories = c
	return nil
}

func (f *Firefly) CachedListCategoryTotals(start, end time.Time) ([]CategoryTotal, error) {
	f.cache.mu.Lock()
	defer f.cache.mu.Unlock()
	key := categoryTotalsKey{
		Start: start,
		End:   end,
	}
	_, ok := f.cache.CategoryTotals[key]
	if !ok {
		err := f.refreshCategoryTotals(key)
		if err != nil {
			return nil, err
		}
	}
	return f.cache.CategoryTotals[key], nil
}

func (f *Firefly) CachedFetchCategoryTotals(catID int, start, end time.Time) ([]CategoryTotal, error) {
	f.cache.mu.Lock()
	defer f.cache.mu.Unlock()
	key := categoryTotalsKey{
		CategoryID: catID,
		Start:      start,
		End:        end,
	}
	_, ok := f.cache.CategoryTotals[key]
	if !ok {
		err := f.refreshCategoryTotals(key)
		if err != nil {
			return nil, err
		}
	}
	return f.cache.CategoryTotals[key], nil
}

func (f *Firefly) refreshCategoryTotals(key categoryTotalsKey) error {
	var (
		c   []CategoryTotal
		err error
	)
	if f.cache.CategoryTotals == nil {
		f.cache.CategoryTotals = make(map[categoryTotalsKey][]CategoryTotal)
	}
	if key.CategoryID == 0 {
		c, err = f.ListCategoryTotals(key.Start, key.End)
	} else {
		c, err = f.FetchCategoryTotal(key.CategoryID, key.Start, key.End)
	}
	if err != nil {
		return fmt.Errorf("could not update category totals cache: %s", err)
	}
	if key.CategoryID != 0 && len(c) != 1 {
		return fmt.Errorf("got %d category totals, wanted 1 for key %d, %s, %s", len(c), key.CategoryID, key.Start, key.End)
	}
	if key.CategoryID == 0 && len(c) == 0 {
		// No category budgets exist.
		return nil
	}
	log.Debug().Msgf("Cache: updating CategoryTotals for key %d, %s, %s", key.CategoryID, key.Start, key.End)
	f.cache.CategoryTotals[key] = c
	return nil
}

func (f *Firefly) CachedTransactions(key TransactionsKey) ([]Transactions, error) {
	f.cache.mu.Lock()
	defer f.cache.mu.Unlock()
	_, ok := f.cache.Transactions[key]
	if !ok {
		err := f.refreshTransactions(key)
		if err != nil {
			return nil, err
		}
	}
	return f.cache.Transactions[key], nil
}

// invalidateTransactionsCache will invalidate all cached transactions
// lists
func (f *Firefly) invalidateTransactionsCache() {
	f.cache.mu.Lock()
	defer f.cache.mu.Unlock()
	log.Debug().Msgf("Cache: clearing Transactions")
	f.cache.Transactions = nil
}

func (f *Firefly) refreshTransactions(key TransactionsKey) error {
	if f.cache.Transactions == nil {
		f.cache.Transactions = make(map[TransactionsKey][]Transactions)
	}
	t, err := f.ListTransactions(key)
	if err != nil {
		return err
	}
	log.Debug().Msgf("Cache: updating Transactions for key %d, %s, %s", key.Page, key.Start, key.End)
	f.cache.Transactions[key] = t
	return nil
}

// refreshCategoryTxnCache will invalidate cache entries related to a particular
// category and time. This should be called after creating a transaction.
func (f *Firefly) refreshCategoryTxnCache(tgt categoryTotalsKey) {
	f.cache.mu.Lock()
	defer f.cache.mu.Unlock()

	for k := range f.cache.CategoryTotals {
		if (k.Start.Year() == tgt.Start.Year() && (k.CategoryID == 0 || k.CategoryID == tgt.CategoryID)) ||
			(k.End.Year() == tgt.End.Year() && (k.CategoryID == 0 || k.CategoryID == tgt.CategoryID)) {
			log.Debug().Msgf("Cache: clearing CategoryTotals for key %d, %s, %s", k.CategoryID, k.Start, k.End)
			delete(f.cache.CategoryTotals, k)
			go func(k categoryTotalsKey) {
				_ = f.refreshCategoryTotals(k)
			}(k)
		}
	}
}
