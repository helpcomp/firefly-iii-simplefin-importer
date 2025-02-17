package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/forPelevin/gomoji"
	"github.com/helpcomp/firefly-iii-simplefin-importer/duration"
	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/helpcomp/firefly-iii-simplefin-importer/prom"
	"github.com/helpcomp/firefly-iii-simplefin-importer/simplefin"
	"github.com/rs/zerolog/log"
	"github.com/sashabaranov/go-openai"
	"github.com/shopspring/decimal"
	"golang.org/x/exp/slices"
	"strconv"
	"strings"
	"time"
)

const _noname = "(no name)"

type OpenAIResponse struct {
	Merchant string `json:"Merchant"`
	Category string `json:"Category"`
}

type ExtractedData struct {
	Company   string
	Category  string
	Skip      bool
	CompanyID string
}

var bypassBalanceCheck []string

// startUpdate initializes the process to update accounts and reconcile balances using Simplefin API and Firefly API.
func startUpdate() {
	// Duration Configuration - How far back to check for transactions
	StartTimeDur, err := duration.ParseDuration(cli.SimplefinLoopbackDuration)
	if err != nil {
		prom.ProgramErrors++
		log.Fatal().Err(err)
	}

	sfin.SetFilter(simplefin.Filter{
		StartDate: time.Now().Add(-StartTimeDur - (24 * time.Hour)).Unix(),
		Pending:   true,
	})

	// Get accounts from Simplefin
	simpleFinAcctResp, err := sfin.Accounts()
	if err != nil {
		prom.APIErrors.Simplefin++
		log.Fatal().Err(err).Msg("Could not initialize SimpleFin API.")
		return
	}
	prom.Sfinresp = simpleFinAcctResp // For Prometheus

	// There are errors in the Simplefin accounts (Actions needed in Simplefin Bridge to restore proper communication)
	for _, acctErr := range simpleFinAcctResp.Errors {
		log.Error().Msgf("%s", acctErr)
		prom.APIErrors.Simplefin++
	}

	// Remove non-existent transactions before looping through new / updated transactions
	// This also prevents balance mismatch
	RemoveNonExistentTransactions(simpleFinAcctResp)

	// Loop through Simplefin Accounts
	for _, acct := range simpleFinAcctResp.Accounts {
		currentAccount, _ := GetAccount(c.Accounts[acct.ID])
		log.Info().Msgf("🏦 Found Account %s (%s) with balance of %s", acct.Name, acct.ID, acct.Balance)

		// Transactions //
		/////////////////
		accountHasPending, pendingBalance := CheckTransactions(acct)

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
				reconcile.SourceName = _noname
				reconcile.Type = "deposit"
			}

			if balanceDifference.LessThan(decimal.NewFromInt(0)) {
				reconcile.SourceID = c.Accounts[acct.ID]
				reconcile.SourceName = ""
				reconcile.DestinationID = ""
				reconcile.DestinationName = currentAccount.Attributes.Name + " reconciliation (USD)"

				if c.NonAssetAccounts[c.Accounts[acct.ID]] != "reconciliation" {
					reconcile.DestinationName = _noname
					reconcile.Type = "withdrawal"
				}
			}

			err = ff.CreateTransaction(reconcile)

			if err != nil {
				prom.APIErrors.Firefly++
				log.Error().Err(err).Msgf("%v : %v", reconcile, err.Error())
				continue
			}
			log.Info().Msgf("Created Account Reconciliation Transaction for %s to match the correct balance of $%s", currentAccount.Attributes.Name, acct.Balance)
			continue
		}

		ExpectedBalance := currentAccount.Attributes.CurrentBalance.Sub(pendingBalance)

		if acct.ID != "" && !acct.Balance.Equal(ExpectedBalance) && !slices.Contains(bypassBalanceCheck, c.Accounts[acct.ID]) {
			log.Error().Msgf("Balance Mismatch for %s! $%s (SimpleFin) != $%s (FireFly). Reconciliation Recommended!", currentAccount.Attributes.Name, acct.Balance, currentAccount.Attributes.CurrentBalance.Sub(pendingBalance))
		}
	}
}

// DoesTransactionExist checks if a given transaction exists in Firefly within a specific time range.
// It returns whether the transaction exists, whether an update is needed, and the ID of the matching transaction if found.
func DoesTransactionExist(newTrans firefly.Transaction) (exists bool, update bool, oldTransactionID string) {
	t, err := duration.ParseDuration(cli.SimplefinLoopbackDuration)
	now := time.Now()
	extendT, _ := duration.ParseDuration("-5d") // This is for transactions that are pending that take a while to post
	then := now.Add(-t).Add(extendT)

	// Unable to parse duration
	if err != nil {
		prom.ProgramErrors++
		log.Fatal().Err(err).Msgf("Unable to format %s to duration. %v", cli.SimplefinLoopbackDuration, err)
		return false, false, ""
	}

	// Get Existing transactions from the cache
	existing, _ := ff.CachedTransactions(firefly.TransactionsKey{
		Start: then.Format(time.DateOnly),
		End:   now.Format(time.DateOnly),
	})

	// Loop through the transactions and compare to newTrans
	for _, ta := range existing {
		// Loop through Transaction Attributes
		for _, oldTrans := range ta.Attributes.Transactions {
			if oldTrans.ExternalID != newTrans.ExternalID {
				continue
			} // Transaction External ID doesn't match, skip

			// Transaction Matches, Needs Update
			oldDate, _ := time.Parse(time.RFC3339, oldTrans.Date)
			if !oldTrans.Amount.Equal(newTrans.Amount) || oldDate.Format(time.DateOnly) != newTrans.Date || !slices.Equal(oldTrans.Tags, newTrans.Tags) || oldTrans.CategoryName == "" || oldTrans.DestinationName == _noname || oldTrans.SourceName == _noname {
				return true, true, ta.ID
			}

			return true, false, ta.ID // Transaction Matches, No Update Needed
		}
	}
	return false, false, "" // No matching transactions found
}

// CheckTransactions processes transactions of a given Simplefin account and reconciles them with Firefly's transactions.
// It determines if there are pending transactions and calculates the pending balance for the account.
// Parameters:
// - acct: a Simplefin account containing transaction and balance details.
// Returns:
// - hasPendingTransactions: indicates if there are pending transactions in the account.
// - pendingBalance: the total balance associated with pending transactions.
func CheckTransactions(acct simplefin.Accounts) (hasPendingTransactions bool, pendingBalance decimal.Decimal) {
	var err error
	skipTransaction := false
	accountHasPending := false
	pendingBalance = decimal.Zero

	// Loop through all the gathered transactions for the Simplefin Account
	for _, trans := range acct.Transactions {
		var tags []string
		newTrans := firefly.Transaction{}

		// Non-Asset Accounts
		_, NonAssetAccount := c.NonAssetAccounts[c.Accounts[acct.ID]]
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
			SourceID:        c.Accounts[acct.ID],
			DestinationName: _noname,
			ExternalID:      trans.ID,
			Tags:            tags,
			Type:            "withdrawal",
		}

		// This is a deposit, flip the Source and Destination
		if trans.Amount.GreaterThan(decimal.NewFromInt(0)) {
			newTrans.DestinationID = c.Accounts[acct.ID]
			newTrans.DestinationName = ""
			newTrans.SourceName = _noname
			newTrans.SourceID = ""
			newTrans.Type = "deposit"
		}

		// Check to see if the transaction already exists, and should we update it
		exists, shouldUpdate, oldTransactionID := DoesTransactionExist(newTrans)

		// Debug Mode - Skip posting and updating transactions
		if cli.DoNotUpdateTransactions {
			if !exists {
				log.Info().Msgf("📜 Found New Transaction %s $%s", trans.Description, trans.Amount)
			}
			continue
		}

		// New Transaction
		if !exists {
			skipTransaction, err = PostTransaction(trans, newTrans)
			if skipTransaction {
				continue
			}

			log.Info().Msgf("📜 Found New Transaction %s $%s", trans.Description, trans.Amount)

			// This handles non-pending transactions never added to SimpleFin
			if !trans.Pending {
				pendingBalance = pendingBalance.Sub(trans.Amount)
			}

			if err != nil {
				// Error Posting Transaction
				prom.APIErrors.Firefly++
				log.Error().Err(err).Msgf("🚨 transaction %s FAILED for %s - %v\n", trans.Description, acct.Name, err)
				continue
			}

			log.Info().Msgf("➕ Successfully added transaction %s on %s\n", trans.Description, acct.Name)
			continue
		}

		// Transaction is pending and already exists in Simplefin, this will need to be subtracted from SimpleFin's balance
		if trans.Pending {
			pendingBalance = pendingBalance.Add(trans.Amount)
		}

		for _, transBypasses := range c.TransactionBypassResponse {
			for key, bypassResp := range transBypasses {
				if bypassResp.Type == "transfer" && key == trans.Description {
					if trans.Pending {
						pendingBalance = pendingBalance.Sub(trans.Amount)
					}
					bypassBalanceCheck = append(bypassBalanceCheck, bypassResp.SourceAccount)
				}
			}
		}

		// Existing Transaction that needs updated
		if shouldUpdate && !trans.Pending {
			log.Info().Msgf("📜 Found Pending Transaction %s $%s", trans.Description, trans.Amount)

			err = UpdateTransaction(oldTransactionID, newTrans, trans)

			if err != nil {
				// Error updating transaction
				prom.APIErrors.Firefly++
				log.Error().Err(err).Msgf("🚨 transaction Update %s FAILED for %s\n", trans.Description, acct.Name)
				continue
			}
			log.Info().Msgf("🖊 Successfully updated transaction %s on %s\n", trans.Description, acct.Name)
		}
	}
	return accountHasPending, pendingBalance
}

// UpdateTransaction updates an existing transaction in Firefly by applying updated details such as source, destination, and category.
// It uses the provided simplefinTransaction to extract company and category data and modifies the ffTransaction accordingly.
// The updated transaction is then sent to Firefly identified by the oldTransactionID. Returns an error if the update fails.
func UpdateTransaction(oldTransactionID string, ffTransaction firefly.Transaction, simplefinTransaction simplefin.Transactions) error {
	extracted := ExtractCompanyAndCategory(simplefinTransaction)

	if ffTransaction.SourceName == _noname {
		ffTransaction.SourceName = extracted.Company
		ffTransaction.SourceID = extracted.CompanyID
	} else {
		ffTransaction.DestinationName = extracted.Company
		ffTransaction.DestinationID = extracted.CompanyID
	}
	ffTransaction.CategoryID = extracted.Category
	ffTransaction.Tags = make([]string, 0) // Remove Tag

	return ff.UpdateTransaction(oldTransactionID, ffTransaction)
}

// PostTransaction processes transactions between SimpleFIN and Firefly and creates or skips them based on specific criteria.
// It extracts merchant and category information, updates transaction details, and applies configuration rules as needed.
// Returns true if the transaction is skipped; otherwise, attempts to create the transaction and returns success status or an error.
func PostTransaction(simplefinTrans simplefin.Transactions, ffTransaction firefly.Transaction) (bool, error) {
	extracted := ExtractCompanyAndCategory(simplefinTrans)

	if extracted.Skip {
		// Skip posting this transaction
		return true, nil
	}

	if ffTransaction.SourceName == _noname {
		ffTransaction.SourceName = extracted.Company
		ffTransaction.SourceID = extracted.CompanyID
	} else {
		ffTransaction.DestinationName = extracted.Company
		ffTransaction.DestinationID = extracted.CompanyID
	}
	ffTransaction.CategoryID = extracted.Category

	// Update Transaction based on Config Data - If Applicable
	for _, transBypasses := range c.TransactionBypassResponse {
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

	return false, ff.CreateTransaction(ffTransaction)
}

// GetAccount retrieves an account from Firefly III based on the provided account ID.
// Returns the matching account or an error if not found.
// Logs errors and increments Prometheus counters for API error tracking.
func GetAccount(accountID string) (account firefly.Account, err error) {
	accounts, err := ff.CachedAccounts()
	if err != nil {
		prom.APIErrors.Firefly++
		log.Error().Err(err).Msgf("Error getting account balanace - %v", err)
		return firefly.Account{}, err
	}

	for _, acct := range accounts {
		if acct.ID != accountID && acct.Attributes.Name != accountID {
			continue
		}
		return acct, nil
	}
	return firefly.Account{}, errors.New("Unable to find an account with the ID of " + accountID)
}

// ExtractCompanyAndCategory processes a transaction to extract the company and category details for classification.
// It uses predefined bypass rules and optionally integrates with OpenAI for enhanced categorization insights.
func ExtractCompanyAndCategory(transaction simplefin.Transactions) ExtractedData {
	var extracted = ExtractedData{
		Company:   _noname,
		Category:  "",
		Skip:      false,
		CompanyID: "",
	}

	var sbCategories, sbAccounts strings.Builder

	if transaction.Description == "" {
		return extracted
	}

	fireflyCategories, _ := ff.CachedCategories()
	fireflyAccounts, _ := ff.CachedAccounts()

	// Loop through Account Names
	for i, aa := range fireflyAccounts {
		if aa.Attributes.Type != "expense" {
			continue
		}

		sbAccounts.WriteString(aa.Attributes.Name)
		if i+1 < len(fireflyAccounts) {
			sbAccounts.WriteString(", ")
		}
	}

	// Loop through Categories
	for i, cc := range fireflyCategories {
		sbCategories.WriteString(cc.Name)
		if i+1 < len(fireflyCategories) {
			sbCategories.WriteString(", ")
		}
	}

	// Check to see if it's a bypassed transaction
	for _, transBypasses := range c.TransactionBypassResponse {
		for key, bypassResp := range transBypasses {
			if strings.Contains(transaction.Description, key) {
				if bypassResp.Skip {
					extracted.Skip = true
					return extracted
				}
				extracted.Company = bypassResp.Company
				extracted.CompanyID = bypassResp.AssetID
				extracted.Category = FindCategoryID(bypassResp.Category, fireflyCategories)
				return extracted
			}
		}
	}

	// If no OpenAI API Key was provided, return default
	if cli.OpenAIAPIKey == "" {
		return extracted
	}

	// OpenAI / ChatGPT
	prompt := fmt.Sprintf("Given the following transaction %s Please respond with JSON using Merchant and Category. \"Merchant\" which is your best guess at the merchant the bank transaction stemmed from using the following list seperated by commas: %s . If a sutable merchant isn't found from the list you can choose your own. when the payment was made via a payment service like paypal only show the merchant name not the payment service used. \"Category\" a general business accounting category you would expect this sort of transaction to be categorized to from the following list seperated by commas: %s . Choose the best category that fits this transaction. Choose only one merchant and category. Do not respond in anything other than JSON, No English unless in JSON format.", transaction.Description, sbAccounts.String(), sbCategories.String())

	req := openai.CompletionRequest{
		Model:     openai.GPT3Dot5TurboInstruct,
		Prompt:    prompt,
		MaxTokens: 256,
	}
	resp, err := oai.CreateCompletion(context.Background(), req)
	if err != nil {
		prom.APIErrors.OpenAI++
		log.Error().Err(err).Msgf("Error with ChatGPT/OpenAI : %v", err)
		return extracted
	}
	prom.OaiUsage = resp.Usage
	prom.APICalls.OpenAI++
	// Split the text by semicolon to get Company and Category
	var rsp OpenAIResponse

	// Try to unmarshal the response into the rsp (OpenAIResponse)
	err = json.Unmarshal([]byte(resp.Choices[0].Text), &rsp)

	// Unmarshal failed, ChatGPT returned an invalid response.
	if err != nil {
		log.Error().Msgf("ChatGPT responded with invalid JSON response.")
		return extracted
	}

	// Unmarshal was successful, ChatGPT returned a valid response
	log.Info().Msgf("🤖 [ChatGPT] Successfully found Company (%s) and Category (%s) for transaction.", rsp.Merchant, rsp.Category)
	extracted.Company = rsp.Merchant
	extracted.Category = FindCategoryID(rsp.Category, fireflyCategories)
	return extracted
}

// FindCategoryID searches for a category by name in a list of firefly.Category and returns its ID as a string.
// The function removes emojis and leading spaces from both input and category names before comparison.
// Returns an empty string if no match is found.
func FindCategoryID(categoryName string, categories []firefly.Category) string {
	for _, cat := range categories {
		if strings.TrimLeft(gomoji.RemoveEmojis(cat.Name), " ") == strings.TrimLeft(gomoji.RemoveEmojis(categoryName), " ") {
			return strconv.Itoa(cat.ID)
		}
	}
	return ""
}

// RemoveNonExistentTransactions removes Firefly transactions
// that no longer exist in SimpleFin within a specified time frame.
func RemoveNonExistentTransactions(accts simplefin.AccountsResponse) {
	log.Info().Msgf("Checking for non-existant transactions")
	t, _ := duration.ParseDuration(cli.SimplefinLoopbackDuration)

	existing, _ := ff.CachedTransactions(firefly.TransactionsKey{
		Start: time.Now().Add(-t).Format(time.DateOnly),
		End:   time.Now().Format(time.DateOnly),
	})

	for _, transAttrib := range existing {
		for _, fireflyTrans := range transAttrib.Attributes.Transactions {
			// Loop through all our Firefly Transactions in the last X (specified from LoopbackDuration) days.
			// Check all transactions in SimpleFin.
			transactionExists := false
			for _, act := range accts.Accounts {
				for _, SimpleFinTrans := range act.Transactions {
					if SimpleFinTrans.ID == fireflyTrans.ExternalID || fireflyTrans.ExternalID == "" {
						transactionExists = true
						break
					}
				}
			}
			if transactionExists {
				continue
			} // Transaction was found, continue to next transaction

			// The Transaction was not found in SimpleFin. It needs to be deleted
			log.Info().Msgf("Transaction %s (%s) doesn't exist in SimpleFin. Removing it.", fireflyTrans.Description, transAttrib.ID)
			err := ff.DeleteTransaction(transAttrib.ID)
			if err != nil {
				log.Error().Err(err).Msgf("Could not delete transaction %s (%s)", fireflyTrans.Description, transAttrib.ID)
			}
		}
	}
}
