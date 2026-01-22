package main

import (
	"errors"

	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/rs/zerolog/log"
)

// GetAccount retrieves an account from Firefly III based on the provided account ID.
// Returns the matching account or an error if not found.
// Logs errors and increments Prometheus counters for API error tracking.
func GetAccount(ff *firefly.Firefly, accountID string) (account firefly.Account, err error) {
	accounts, err := ff.CachedAccounts()
	if err != nil {
		log.Error().Err(err).Msgf("Error getting accounts")
		return firefly.Account{}, err
	}

	// Try ID first
	if acct, ok := accounts.AccountsByID[accountID]; ok {
		return acct, nil
	}

	// Try name second
	if acct, ok := accounts.AccountsByName[accountID]; ok {
		return acct, nil
	}

	return firefly.Account{}, errors.New("unable to find an account")
}
