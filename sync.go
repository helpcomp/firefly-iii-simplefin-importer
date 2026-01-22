package main

import (
	"time"

	"github.com/helpcomp/firefly-iii-simplefin-importer/config"
	"github.com/helpcomp/firefly-iii-simplefin-importer/duration"
	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/helpcomp/firefly-iii-simplefin-importer/simplefin"
	"github.com/rs/zerolog/log"
	"github.com/sashabaranov/go-openai"
	"github.com/shopspring/decimal"
)

// startUpdate initializes the process to update accounts and reconcile balances using Simplefin API and Firefly API.
func startUpdate(sf *simplefin.Simplefin, ff *firefly.Firefly, c *config.MasterConfig, oai *openai.Client) []simplefin.Accounts {
	log.Debug().Msg("Starting Simplefin Update")
	// Duration Configuration - How far back to check for transactions
	StartTimeDur, err := duration.ParseDuration(cli.SimplefinLoopbackDuration)
	if err != nil {
		log.Fatal().Err(err)
	}
	pendingTransfers := make(map[string]decimal.Decimal) // Reset Pending Transactions

	sf.SetFilter(simplefin.Filter{
		StartDate: time.Now().Add(-StartTimeDur - (24 * time.Hour)).Unix(),
		Pending:   true,
	})

	log.Debug().Msgf("Retreiving Simplefin Account Data")
	// Get accounts from Simplefin
	simpleFinAcctResp, err := sf.Accounts()
	if err != nil {
		log.Error().Err(err).Msg("Could not initialize SimpleFin API.")
		return nil
	}

	// There are errors in the Simplefin accounts (Actions needed in Simplefin Bridge to restore proper communication)
	for _, acctErr := range simpleFinAcctResp.Errors {
		log.Error().Msgf("%s", acctErr)
	}

	// Remove non-existent transactions before looping through new / updated transactions
	// This also prevents balance mismatch
	RemoveNonExistentTransactions(ff, simpleFinAcctResp)

	// Create SyncApp once for all accounts (avoids rebuilding transferBypasses map for each account)
	syncApp := NewSyncApp(ff, c, oai)

	// Loop through Simplefin Accounts
	for _, acct := range simpleFinAcctResp.Accounts {
		if c.Accounts[acct.ID] == "0" {
			continue
		}
		currentAccount, err := GetAccount(ff, c.Accounts[acct.ID])
		if err != nil {

			if err.Error() == "unable to find an account" {
				log.Warn().Err(err).Str("AccountID", acct.ID).Str("AccountName", acct.Name).Msgf("Unable to get account from FireFly. If this is expected, please add the AccountID to the config.yaml file as %s: 0", acct.ID)
				continue
			}

			log.Error().Err(err).Str("AccountID", acct.ID).Str("AccountName", acct.Name).Msgf("Error getting account from FireFly")
			continue
		}

		log.Info().
			Str("Type", "Account").
			Str("Name", acct.Name).
			Str("ID", acct.ID).
			Float64("Balance", acct.Balance.InexactFloat64()).
			Msg("🏦 Found Simplefin Account")

		// Transactions //
		/////////////////
		accountHasPending, pendingBalance := CheckTransactions(syncApp, acct, pendingTransfers)

		// Account Reconciliation //
		///////////////////////////
		_, ok := c.NonAssetAccounts[c.Accounts[acct.ID]]
		// Reconcile Account if there are no pending transactions, the balance doesn't match, and EnableReconciliation is true
		if !accountHasPending && !currentAccount.Attributes.CurrentBalance.Equal(acct.Balance) && (cli.FireflyEnableReconciliation || ok) {
			balanceDifference := acct.Balance.Sub(currentAccount.Attributes.CurrentBalance)
			reconcile := firefly.Transaction{
				Date:          time.Now().Format(time.DateOnly),
				Amount:        balanceDifference.Abs(),
				Description:   "Account Reconciliation",
				DestinationID: c.Accounts[acct.ID],
				SourceName:    currentAccount.Attributes.Name + " reconciliation (USD)",
				Type:          "reconciliation",
			}

			if c.NonAssetAccounts[c.Accounts[acct.ID]] != "reconciliation" {
				reconcile.SourceName = defaultAccountName
				reconcile.Type = "deposit"
			}

			if balanceDifference.LessThan(decimal.NewFromInt(0)) {
				reconcile.SourceID = c.Accounts[acct.ID]
				reconcile.SourceName = ""
				reconcile.DestinationID = ""
				reconcile.DestinationName = currentAccount.Attributes.Name + " reconciliation (USD)"

				if c.NonAssetAccounts[c.Accounts[acct.ID]] != "reconciliation" {
					reconcile.DestinationName = defaultAccountName
					reconcile.Type = "withdrawal"
				}
			}

			err = ff.CreateTransaction(reconcile)

			if err != nil {
				log.Error().Err(err).Msgf("%v : %v", reconcile, err.Error())
				continue
			}
			log.Info().Str("Type", "Reconciliation").Str("Name", currentAccount.Attributes.Name).Str("ID", currentAccount.ID).Float64("Balance", acct.Balance.InexactFloat64()).Msgf("Reconciled Account %s", acct.Name)
			continue
		}

		ExpectedBalance := currentAccount.Attributes.CurrentBalance.Sub(pendingBalance)

		if currentAccount.ID != "" && acct.ID != "" && !acct.Balance.Equal(ExpectedBalance) {
			var pendBal decimal.Decimal
			if val, ok := pendingTransfers[c.Accounts[acct.ID]]; ok {
				pendBal = ExpectedBalance.Add(val)
			}

			if !acct.Balance.Equal(pendBal) {
				log.Error().Str("Type", "BalanceMismatch").Str("Name", currentAccount.Attributes.Name).Str("ID", currentAccount.ID).Float64("Expected", ExpectedBalance.InexactFloat64()).Float64("Actual", acct.Balance.InexactFloat64()).Msgf("Balance Mismatch for %s!", currentAccount.Attributes.Name)
			}
		}
	}
	return simpleFinAcctResp.Accounts
}
