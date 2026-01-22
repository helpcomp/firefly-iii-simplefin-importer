package main

import (
	"time"

	"github.com/helpcomp/firefly-iii-simplefin-importer/duration"
	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/helpcomp/firefly-iii-simplefin-importer/simplefin"
	"github.com/rs/zerolog/log"
)

// RemoveNonExistentTransactions removes Firefly transactions
// that no longer exist in SimpleFin within a specified time frame.
func RemoveNonExistentTransactions(ff *firefly.Firefly, accountsResponse simplefin.AccountsResponse) {
	log.Debug().Msgf("Checking for non-existant transactions")
	t, err := duration.ParseDuration(cli.SimplefinLoopbackDuration)
	if err != nil {
		log.Error().Err(err).Msg("Error parsing Simplefin Loopback Duration")
		return
	}

	existing, err := ff.CachedTransactions(firefly.TransactionsKey{
		Start: time.Now().Add(-t).Format(time.DateOnly),
		End:   time.Now().Format(time.DateOnly),
	})

	if err != nil {
		log.Error().Err(err).Msg("Error getting cached transactions")
		return
	}

	// BUILD INDEX: O(p×q) = ~100 iterations
	simpleFinIDs := make(map[string]bool)
	for _, act := range accountsResponse.Accounts {
		for _, trans := range act.Transactions {
			simpleFinIDs[trans.ID] = true
		}
	}

	for _, transAttrib := range existing {
		for _, fireflyTrans := range transAttrib.Attributes.Transactions {
			// Loop through all our Firefly Transactions in the last X (specified from LoopbackDuration) days.
			// Check all transactions in SimpleFin.

			// If there is no ExternalID, skip the transaction
			if fireflyTrans.ExternalID == "" {
				log.Info().Msgf("Transaction %s (%s) doesn't exist in SimpleFin. Missing ExternalID; It was probably added manually, skipping removal.", fireflyTrans.Description, transAttrib.ID)
				continue
			}

			// lookup
			if simpleFinIDs[fireflyTrans.ExternalID] {
				continue // Transaction exists
			}

			// The Transaction was not found in SimpleFin. It needs to be deleted
			if !cli.AutoRemoveTransactions {
				// Auto Removal is turned off, alert only.
				log.Info().Str("Type", "Transaction").Str("Description", fireflyTrans.Description).Str("ID", transAttrib.ID).Msg("Transaction doesn't exist in SimpleFin. [AutoRemove is Off]")
				continue
			}

			// Auto Removal is enabled, proceed with removing the transaction.
			log.Info().Str("Type", "Transaction").Str("Description", fireflyTrans.Description).Str("ID", transAttrib.ID).Msg("Transaction doesn't exist in SimpleFin. It will be removed.")
			err = ff.DeleteTransaction(transAttrib.ID)
			if err != nil {
				log.Error().Err(err).Msgf("Could not delete transaction %s (%s)", fireflyTrans.Description, transAttrib.ID)
			}
		}
	}
}
