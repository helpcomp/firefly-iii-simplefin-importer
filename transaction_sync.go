package main

import (
	"strings"
	"time"

	"github.com/helpcomp/firefly-iii-simplefin-importer/config"
	"github.com/helpcomp/firefly-iii-simplefin-importer/duration"
	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/helpcomp/firefly-iii-simplefin-importer/simplefin"
	"github.com/rs/zerolog/log"
	"github.com/sashabaranov/go-openai"
	"github.com/shopspring/decimal"
	"golang.org/x/exp/slices"
)

type SyncApp struct {
	firefly          *firefly.Firefly
	config           *config.MasterConfig
	oai              *openai.Client
	transferBypasses map[string]config.TransactionInfo
}

// NewSyncApp creates a new SyncApp instance with a pre-built transfer bypass map.
// This should be created once and reused across multiple accounts for efficiency.
func NewSyncApp(ff *firefly.Firefly, cfg *config.MasterConfig, oai *openai.Client) *SyncApp {
	return &SyncApp{
		firefly:          ff,
		config:           cfg,
		oai:              oai,
		transferBypasses: buildTransferBypassMap(cfg),
	}
}

type TransactionIndex struct {
	Exists        bool
	NeedsUpdate   bool
	TransactionID string
	OldTrans      firefly.Transaction
}

// DoesTransactionExist checks if a given transaction exists in Firefly within a specific time range.
// It returns whether the transaction exists, whether an update is needed, and the ID of the matching transaction if found.
func (s *SyncApp) DoesTransactionExist(newTrans firefly.Transaction, transIndex map[string]TransactionIndex) (exists bool, update bool, oldTransactionID string) {
	idx, found := transIndex[newTrans.ExternalID]

	if !found {
		return false, false, "" // No matching transaction
	}

	// Check if update is needed
	oldDate, err := time.Parse(time.RFC3339, idx.OldTrans.Date)

	if err != nil {
		log.Error().Err(err).Msgf("Unable to parse date")
		return true, false, idx.TransactionID
	}

	if !idx.OldTrans.Amount.Equal(newTrans.Amount) ||
		oldDate.Format(time.DateOnly) != newTrans.Date ||
		!slices.Equal(idx.OldTrans.Tags, newTrans.Tags) ||
		idx.OldTrans.CategoryName == "" ||
		idx.OldTrans.DestinationName == defaultAccountName ||
		idx.OldTrans.SourceName == defaultAccountName {
		return true, true, idx.TransactionID
	}

	return true, false, idx.TransactionID // Transaction matches, no update needed
}

// CheckTransactions processes transactions of a given Simplefin account and reconciles them with Firefly's transactions.
// It determines if there are pending transactions and calculates the pending balance for the account.
// Parameters:
// - s: SyncApp instance containing Firefly client, config, and OpenAI client
// - acct: a Simplefin account containing transaction and balance details
// - pendingTransfers: map tracking pending transfer amounts per account
// Returns:
// - hasPendingTransactions: indicates if there are pending transactions in the account
// - pendingBalance: the total balance associated with pending transactions
func CheckTransactions(s *SyncApp, acct simplefin.Accounts, pendingTransfers map[string]decimal.Decimal) (hasPendingTransactions bool, pendingBalance decimal.Decimal) {
	var err error
	skipTransaction := false
	accountHasPending := false
	pendingBalance = decimal.Zero
	processedTransactions := 0

	// Build Index
	t, err := duration.ParseDuration(cli.SimplefinLoopbackDuration)
	if err != nil {
		log.Fatal().Err(err).Msgf("Unable to parse duration")
		return false, decimal.Zero
	}
	now := time.Now()
	extendT, _ := duration.ParseDuration("-5d")
	then := now.Add(-t).Add(extendT)

	existing, err := s.firefly.CachedTransactions(firefly.TransactionsKey{
		Start: then.Format(time.DateOnly),
		End:   now.Format(time.DateOnly),
	})
	if err != nil {
		log.Error().Err(err).Msg("Error getting cached transactions")
		return false, decimal.Zero
	}

	// Build the index once for all transactions
	transIndex := buildTransactionIndex(existing)

	// Loop through all the gathered transactions for the Simplefin Account
	for _, trans := range acct.Transactions {
		var tags []string
		newTrans := firefly.Transaction{}

		// Non-Asset Accounts
		_, NonAssetAccount := s.config.NonAssetAccounts[s.config.Accounts[acct.ID]]
		if NonAssetAccount {
			continue
		}

		// This is a Pending Transaction, add the Pending tag and mark the account as having a pending transaction
		// As to not trigger a Reconciliation Necessary alert
		if trans.Pending {
			tags = append(tags, "Pending")
			accountHasPending = true
		}
		// Create a Firefly Transaction based on the Simplefin Transaction
		newTrans = firefly.Transaction{
			Date:            time.Unix(trans.TransactedAt, 0).Format(time.DateOnly),
			Amount:          trans.Amount.Abs(),
			Description:     trans.Description,
			SourceName:      acct.Name,
			SourceID:        s.config.Accounts[acct.ID],
			DestinationName: defaultAccountName,
			ExternalID:      trans.ID,
			Tags:            tags,
			Type:            "withdrawal",
		}

		// This is a deposit, flip the Source and Destination
		if trans.Amount.GreaterThan(decimal.NewFromInt(0)) {
			newTrans.DestinationID = s.config.Accounts[acct.ID]
			newTrans.DestinationName = ""
			newTrans.SourceName = defaultAccountName
			newTrans.SourceID = ""
			newTrans.Type = "deposit"
		}

		// Check to see if the transaction already exists, and should we update it
		exists, shouldUpdate, oldTransactionID := s.DoesTransactionExist(newTrans, transIndex)

		//log.Info().Msgf("📜 Found Transaction %s $%s", trans.Description, trans.Amount)

		// Debug Mode - Skip posting and updating transactions
		if cli.DoNotUpdateTransactions {
			if !exists {
				extracted := ExtractCompanyAndCategory(s.firefly, s.config, s.oai, trans)

				if newTrans.SourceName == defaultAccountName {
					newTrans.SourceName = extracted.Company
					newTrans.SourceID = extracted.CompanyID
				} else {
					newTrans.DestinationName = extracted.Company
					newTrans.DestinationID = extracted.CompanyID
				}
				newTrans.CategoryID = extracted.Category

				//log.Info().Msgf("📜 Found New Transaction %s $%s [%s -> %s]", trans.Description, trans.Amount, newTrans.SourceName, newTrans.DestinationName)

				// This handles non-pending transactions never added to SimpleFin
				if !trans.Pending {
					pendingBalance = pendingBalance.Sub(trans.Amount)
				}
			}

			log.Info().
				Str("Type", "Transaction").
				Str("Description", trans.Description).
				Str("ID", trans.ID).
				Bool("Exists", exists).
				Str("Source", newTrans.SourceName).
				Str("Destination", newTrans.DestinationName).
				Float64("Amount", trans.Amount.InexactFloat64()).
				Msg("📜 Found Transaction (Debug Mode) - Not Updating Transaction")
			continue
		}

		// New Transaction
		if !exists {
			skipTransaction, err = s.PostTransaction(trans, newTrans)
			if skipTransaction {
				continue
			}

			// This handles non-pending transactions never added to SimpleFin
			if !trans.Pending {
				pendingBalance = pendingBalance.Sub(trans.Amount)
			}

			log.Info().
				Str("Type", "Transaction").
				Str("Description", trans.Description).
				Str("ID", trans.ID).
				Bool("Exists", exists).
				Str("Source", newTrans.SourceName).
				Str("Destination", newTrans.DestinationName).
				Float64("Amount", trans.Amount.InexactFloat64()).
				Msg("📜 Found Transaction")

			if err != nil {
				// Error Posting Transaction
				log.Error().Err(err).Msgf("🚨 transaction %s FAILED for %s - %v\n", trans.Description, acct.Name, err)
				continue
			}
			processedTransactions++
			log.Info().Str("type", "Add").Str("transactionDescription", trans.Description).Str("accountName", acct.Name).Msgf("➕ Successfully added transaction")
			continue
		}

		// Calculate the pending balance for the existing transaction and account
		pendingBalance = pendingBalance.Add(s.CalculatePendingBalance(trans, acct, pendingTransfers))

		// Existing Transaction that needs updated
		if shouldUpdate && !trans.Pending {
			err = s.UpdateTransaction(oldTransactionID, newTrans, trans)

			if err != nil {
				// Error updating transaction
				log.Error().Err(err).Msgf("🚨 transaction Update %s FAILED for %s\n", trans.Description, acct.Name)
				continue
			}

			processedTransactions++
		}
	}

	log.Info().
		Float64("PendingBalance", pendingBalance.InexactFloat64()).
		Int("ProcessedTransactions", processedTransactions).
		Str("Account", acct.Name).
		Bool("Errors", err != nil).
		Msgf("Processed Transactions")

	return accountHasPending, pendingBalance
}

// CalculatePendingBalance computes the pending balance for a given account by analyzing its transactions and pending transfers.
// Transactions that are marked as pending or transfers bypassed by configuration are included in the calculation.
// Returns the updated pending balance as a decimal.Decimal value.
func (s *SyncApp) CalculatePendingBalance(trans simplefin.Transactions, account simplefin.Accounts, pendingTransfers map[string]decimal.Decimal) decimal.Decimal {
	var pendingBalance decimal.Decimal
	// Transaction is pending and already exists in Simplefin, this will need to be subtracted from SimpleFin's balance
	if trans.Pending {
		pendingBalance = pendingBalance.Add(trans.Amount)
		pendingTransfers[s.config.Accounts[account.ID]] = pendingTransfers[s.config.Accounts[account.ID]].Add(trans.Amount)
	}

	if bypassResp, found := s.transferBypasses[trans.Description]; found {
		pendingTransfers[bypassResp.SourceAccount] = pendingTransfers[bypassResp.SourceAccount].Add(trans.Amount)
		if trans.Pending {
			pendingBalance = pendingBalance.Sub(trans.Amount)
			pendingTransfers[bypassResp.SourceAccount] = pendingTransfers[bypassResp.SourceAccount].Add(trans.Amount)
		}
	}

	return pendingBalance
}

// UpdateTransaction updates an existing transaction in Firefly by applying updated details such as source, destination, and category.
// It uses the provided simplefinTransaction to extract company and category data and modifies the ffTransaction accordingly.
// The updated transaction is then sent to Firefly identified by the oldTransactionID. Returns an error if the update fails.
func (s *SyncApp) UpdateTransaction(oldTransactionID string, ffTransaction firefly.Transaction, simplefinTransaction simplefin.Transactions) error {
	extracted := ExtractCompanyAndCategory(s.firefly, s.config, s.oai, simplefinTransaction)

	if ffTransaction.SourceName == defaultAccountName {
		ffTransaction.SourceName = extracted.Company
		ffTransaction.SourceID = extracted.CompanyID
	} else {
		ffTransaction.DestinationName = extracted.Company
		ffTransaction.DestinationID = extracted.CompanyID
	}
	ffTransaction.CategoryID = extracted.Category
	ffTransaction.Tags = make([]string, 0) // Remove Tag

	return s.firefly.UpdateTransaction(oldTransactionID, ffTransaction)
}

// PostTransaction processes transactions between SimpleFIN and Firefly and creates or skips them based on specific criteria.
// It extracts merchant and category information, updates transaction details, and applies configuration rules as needed.
// Returns true if the transaction is skipped; otherwise, attempts to create the transaction and returns success status or an error.
func (s *SyncApp) PostTransaction(simplefinTrans simplefin.Transactions, ffTransaction firefly.Transaction) (bool, error) {
	extracted := ExtractCompanyAndCategory(s.firefly, s.config, s.oai, simplefinTrans)

	if extracted.Skip {
		// Skip posting this transaction
		return true, nil
	}

	if ffTransaction.SourceName == defaultAccountName {
		ffTransaction.SourceName = extracted.Company
		ffTransaction.SourceID = extracted.CompanyID
	} else {
		ffTransaction.DestinationName = extracted.Company
		ffTransaction.DestinationID = extracted.CompanyID
	}
	ffTransaction.CategoryID = extracted.Category

	// Update Transaction based on Config Data - If Applicable
	for _, transBypasses := range s.config.TransactionBypassResponse {
		for key, configResp := range transBypasses {
			if strings.Contains(simplefinTrans.Description, key) {
				// Update Type
				if configResp.Type != "" {
					ffTransaction.Type = configResp.Type
				}

				// Update Source Account
				if configResp.SourceAccount != "" {
					ffTransaction.SourceID = configResp.SourceAccount
					ffTransaction.SourceName = ""
				}

				// Update Destination Account
				if configResp.DestinationAccount != "" {
					ffTransaction.DestinationID = configResp.DestinationAccount
					ffTransaction.DestinationName = ""
				}
				break
			}
		}
	}

	return false, s.firefly.CreateTransaction(ffTransaction)
}

func buildTransactionIndex(existing []firefly.Transactions) map[string]TransactionIndex {
	index := make(map[string]TransactionIndex)

	for _, ta := range existing {
		for _, oldTrans := range ta.Attributes.Transactions {
			if oldTrans.ExternalID != "" {
				index[oldTrans.ExternalID] = TransactionIndex{
					Exists:        true,
					TransactionID: ta.ID,
					OldTrans:      oldTrans,
				}
			}
		}
	}

	return index
}

// buildTransferBypassMap constructs a map of transaction bypass information specifically for transfer-type transactions.
func buildTransferBypassMap(cfg *config.MasterConfig) map[string]config.TransactionInfo {
	transferMap := make(map[string]config.TransactionInfo)

	for _, transBypasses := range cfg.TransactionBypassResponse {
		for key, bypassResp := range transBypasses {
			// Only add transfer type bypasses to the map
			if bypassResp.Type == "transfer" {
				transferMap[key] = bypassResp
			}
		}
	}

	return transferMap
}
