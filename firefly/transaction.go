package firefly

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/helpcomp/firefly-iii-simplefin-importer/httperror"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

type TxnsResponse struct {
	Data  []Transactions `json:"data"`
	Meta  meta           `json:"meta"`
	Links links          `json:"links"`
}

type Transactions struct {
	ID         string                `json:"id"`
	Attributes TransactionAttributes `json:"attributes"`
}

type TransactionAttributes struct {
	GroupTitle   string        `json:"group_title"`
	Transactions []Transaction `json:"transactions"`
}

type Transaction struct {
	Type            string          `json:"type"`
	Date            string          `json:"date"` // "2018-09-17T12:46:47+01:00"
	Amount          decimal.Decimal `json:"amount"`
	Description     string          `json:"description"`
	CategoryID      string          `json:"category_id,omitempty"`
	CategoryName    string          `json:"category_name"`
	SourceID        string          `json:"source_id,omitempty"`
	SourceName      string          `json:"source_name,omitempty"`
	DestinationID   string          `json:"destination_id,omitempty"`
	DestinationName string          `json:"destination_name,omitempty"`
	Tags            []string        `json:"tags"`
	ExternalID      string          `json:"external_id,omitempty"`
}

type createRequest struct {
	ErrorIfDuplicate bool          `json:"error_if_duplicate_hash"`
	Transactions     []Transaction `json:"transactions"`
}

type updateRequest struct {
	Transactions []Transaction `json:"transactions"`
}

func (f *Firefly) CreateTransaction(t Transaction) error {
	// Validate the transaction (this modifies t.CategoryID and t.Type if needed)
	txnDate, err := ValidateTransaction(&t, f)
	if err != nil {
		return err
	}

	// Additional check: ensure a transaction type was determined
	if t.Type == "" {
		return errors.New(fmt.Sprintf("Could not determine transaction type with provided account information: sourceID: %s, sourceName: %s; destID: %s, destName: %s\n", t.SourceID, t.SourceName, t.DestinationID, t.DestinationName))
	}

	// Send it to the firefly API
	doc := createRequest{
		ErrorIfDuplicate: false,
		Transactions:     []Transaction{t},
	}

	const path = "/api/v1/transactions"

	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	r, _ := http.NewRequest("POST", f.url+path, bytes.NewBuffer(body))
	r.Header.Add("Authorization", "Bearer "+f.token)
	r.Header.Add("Content-Type", "application/json")
	resp, err := f.client.Do(r)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf("Could not create transaction, got status %d %s", resp.StatusCode, resp.Status))
	}

	// Check for a successful response
	var result struct {
		Data Transactions `json:"data"`
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return err
	}

	if result.Data.ID == "" {
		return errors.New(fmt.Sprintf("Could not create transaction"))
	}

	// Invalidate any matching cache entries. Since the transaction was
	// successfully created, the conversions should not raise errors
	catID, _ := strconv.Atoi(t.CategoryID)
	key := categoryTotalsKey{
		CategoryID: catID,
		Start:      txnDate,
		End:        txnDate,
	}
	f.invalidateTransactionsCache() // since the user is going to txns page next, update now
	go func() {                     // we can update other caches after returning
		f.refreshCategoryTxnCache(key)
		_ = f.refreshAccounts()
	}()
	// Successful txn creation should redirect the client to the transaction page
	return nil
}

// DeleteTransaction deletes a transaction by its ID from the system using a DELETE HTTP request.
// Returns an error if the transaction ID is invalid, the request fails, or the response status is not 204 No Content.
func (f *Firefly) DeleteTransaction(transID string) error {
	// Verify that a transaction ID is provided
	if transID == "" {
		return fmt.Errorf("no Transaction ID Provided")
	}

	const path = "/api/v1/transactions/"

	r, _ := http.NewRequest("DELETE", f.url+path+transID, nil)

	r.Header.Add("Authorization", "Bearer "+f.token)
	r.Header.Add("Content-Type", "application/json")
	resp, err := f.client.Do(r)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// Error deleting transaction
	if resp.StatusCode != http.StatusNoContent {
		return errors.New(fmt.Sprintf("Could not delete transaction, got status %d %s", resp.StatusCode, resp.Status))
	}

	f.invalidateTransactionsCache() // Clear Transactions cache

	// Successful transaction deletion
	return nil
}

func (f *Firefly) UpdateTransaction(transID string, t Transaction) error {
	if transID == "" {
		return errors.New("missing Transaction ID")
	}

	// Validate the transaction (this modifies t.CategoryID and t.Type if needed)
	txnDate, err := ValidateTransaction(&t, f)
	if err != nil {
		return err
	}

	// Send it to the firefly API
	doc := updateRequest{
		Transactions: []Transaction{t},
	}

	const path = "/api/v1/transactions/"
	body, err := json.Marshal(doc)
	if err != nil {
		return err

	}

	r, _ := http.NewRequest("PUT", f.url+path+transID, bytes.NewBuffer(body))
	r.Header.Add("Authorization", "Bearer "+f.token)
	r.Header.Add("Content-Type", "application/json")
	resp, err := f.client.Do(r)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return errors.New(fmt.Sprintf("Could not update transaction, got status %d %s", resp.StatusCode, resp.Status))
	}

	// Check for a successful response
	var result struct {
		Data Transactions `json:"data"`
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return err
	}
	if result.Data.ID == "" {
		return errors.New(fmt.Sprintf("Could not update transaction"))
	}

	// Invalidate any matching cache entries. Since the transaction was
	// successfully created, the conversions should not raise errors
	catID, _ := strconv.Atoi(t.CategoryID)
	key := categoryTotalsKey{
		CategoryID: catID,
		Start:      txnDate,
		End:        txnDate,
	}
	f.invalidateTransactionsCache() // since the user is going to txns page next, update now
	go func() {                     // we can update other caches after returning
		f.refreshCategoryTxnCache(key)
		_ = f.refreshAccounts()
	}()
	// Successful txn creation should redirect the client to the transaction page
	return nil
}

// resolveAccount will determine the ID of an account, provided a name; or the
// name, provided an ID.
func (f *Firefly) resolveAccount(id, name string) (string, string) {
	// Both name and ID missing or provided
	if (id == "" && name == "") || (id != "" && name != "") {
		return id, name
	}

	accounts, err := f.CachedAccounts()

	if err != nil {
		log.Error().Err(err).Msgf("Could not get cached accounts")
		return id, name
	}

	// Name provided
	if id == "" {
		return accounts.AccountsByName[name].ID, name
	}

	return accounts.AccountsByID[id].Attributes.Name, id
}

const (
	AcctTypeAsset   = "asset"
	AcctTypeExpense = "expense"
	AcctTypeRevenue = "revenue"
)

// calcTxnType determines whether the transaction is a deposit, withdrawal, or
// transfer. If source is an asset account and dest is not: withdrawal. If dest
// is an asset account but source is not: deposit. If both accounts are of the
// same type: transfer.
//
// TODO(davidschlachter): this may be confused if we have two accounts with the
// same name but different types, e.g. expense and revenue
func (f *Firefly) calcTxnType(srcID, srcName, destID, destName string) string {
	srcID, srcName = f.resolveAccount(srcID, srcName)
	destID, destName = f.resolveAccount(destID, destName)
	var srcType, destType string
	acts, err := f.CachedAccounts()
	if err != nil {
		log.Error().Err(err).Msgf("Error getting cached accounts")
		return ""
	}

	// Find type of existing accounts
	for _, a := range acts.Accounts {
		switch a.ID {
		case srcID:
			srcType = a.Attributes.Type
		case destID:
			destType = a.Attributes.Type
		}
		if srcType != "" && destType != "" {
			break
		}
	}
	// TODO(davidschlachter): maybe support cash accounts one day
	if srcType == "" && srcName != "" && destType == AcctTypeAsset {
		srcType = AcctTypeRevenue
	}
	if destType == "" && destName != "" && srcType == AcctTypeAsset {
		destType = AcctTypeExpense
	}
	// Determine a transaction type
	if srcType == AcctTypeAsset && (destType == AcctTypeExpense || destType == AcctTypeRevenue) {
		return "withdrawal"
	} else if (srcType == AcctTypeRevenue || srcType == AcctTypeExpense) && destType == AcctTypeAsset {
		return "deposit"
	} else if srcType == AcctTypeAsset && destType == AcctTypeAsset {
		return "transfer"
	} else {
		return ""
	}
}

func (f *Firefly) listTxns(w http.ResponseWriter, req *http.Request) {
	var (
		page       int
		start, end string
	)
	pageStr, ok := req.URL.Query()["page"]
	if ok && len(pageStr) > 0 {
		page, _ = strconv.Atoi(pageStr[0]) // if the page cannot be parsed, we'll return page 1
	}

	startStr, ok := req.URL.Query()["start"]
	if ok && len(startStr) > 0 {
		start = startStr[0]
	}

	endStr, ok := req.URL.Query()["end"]
	if ok && len(endStr) > 0 {
		end = endStr[0]
	}

	key := TransactionsKey{
		Page:  page,
		Start: start,
		End:   end,
	}

	txns, err := f.CachedTransactions(key)
	if err != nil {
		httperror.Send(w, req, http.StatusInternalServerError, fmt.Sprintf("Could not list transactions: %s", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(txns)
	if err != nil {
		return
	}
}

func (f *Firefly) ListTransactions(key TransactionsKey) ([]Transactions, error) {
	const path = "/api/v1/transactions"
	var (
		page               int
		results            []Transactions
		params, dateParams string
	)

	if key.Start != "" && key.End != "" {
		dateParams = fmt.Sprintf("start=%s&end=%s", key.Start, key.End)
		page = 1
	} else {
		if key.Page == 0 {
			key.Page = 1
		}
		page = key.Page
	}

	for more := true; more; page++ {
		if dateParams != "" {
			params = fmt.Sprintf("page=%d&%s", page, dateParams)
		} else {
			params = fmt.Sprintf("page=%d", page)
		}
		req, _ := http.NewRequest("GET", f.url+path+"?"+params, nil)
		req.Header.Add("Authorization", "Bearer "+f.token)
		resp, err := f.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Transactions: %s", err)
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("got status %d", resp.StatusCode)
		}

		var txns TxnsResponse
		err = json.NewDecoder(resp.Body).Decode(&txns)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}

		results = append(results, txns.Data...)
		more = txns.Meta.Pagination.CurrentPage < txns.Meta.Pagination.TotalPages
	}
	return results, nil
}

func (f *Firefly) ListTransactionsNew(key TransactionsKey) (TxnsResponse, error) {
	const path = "/api/v1/transactions"
	var (
		params string
	)
	if key.Type != "" {
		params = fmt.Sprintf("type=%s", key.Type)
	}

	req, _ := http.NewRequest("GET", f.url+path+"?"+params, nil)
	req.Header.Add("Authorization", "Bearer "+f.token)
	resp, err := f.client.Do(req)
	if err != nil {
		return TxnsResponse{}, fmt.Errorf("failed to fetch Transactions: %s", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return TxnsResponse{}, fmt.Errorf("got status %d", resp.StatusCode)
	}

	var txns TxnsResponse
	err = json.NewDecoder(resp.Body).Decode(&txns)
	if err != nil {
		return TxnsResponse{}, err
	}
	return txns, nil
}

func (f *Firefly) fetchTxn(w http.ResponseWriter, req *http.Request) {
	id := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
	if _, err := strconv.Atoi(id); err != nil {
		httperror.Send(w, req, http.StatusBadRequest, fmt.Sprintf("Could not parse transaction ID: %s", id))
		return
	}

	txn, err := f.FetchTransaction(id)
	if err != nil {
		httperror.Send(w, req, http.StatusInternalServerError, fmt.Sprintf("Could not fetch transaction: %s", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(txn)
	if err != nil {
		log.Error().Err(err).Msgf("Could not encode transaction")
		return
	}
}

func (f *Firefly) FetchTransaction(id string) (*Transactions, error) {
	const path = "/api/v1/transactions/"

	req, _ := http.NewRequest("GET", f.url+path+id, nil)
	req.Header.Add("Authorization", "Bearer "+f.token)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Transaction: %s", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got status %d", resp.StatusCode)
	}

	var result struct {
		Data Transactions `json:"data"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return nil, err
	}

	if result.Data.ID == "" {
		return nil, fmt.Errorf("no transaction found")
	}

	return &result.Data, nil
}

func ValidateTransaction(t *Transaction, f *Firefly) (time.Time, error) {
	//
	// Validate the request
	//
	// Verify that a provided category ID is valid. If only a category name is
	// provided, add the ID. Allow an empty category (e.g., for a transfer).
	if t.CategoryID != "" || t.CategoryName != "" {
		cats, err := f.CachedCategories()

		if err != nil {
			log.Error().Err(err).Msgf("Error getting cached categories")
			return time.Now(), err
		}

		var ok bool
		for _, c := range cats {
			if strconv.Itoa(c.ID) == t.CategoryID {
				ok = true
				break
			}
			if c.Name == t.CategoryName {
				t.CategoryID = strconv.Itoa(c.ID)
				ok = true
				break
			}
		}
		if !ok {
			return time.Now(), fmt.Errorf("could not find Category with ID = '%s' or Name = '%s'", t.CategoryID, t.CategoryName)
		}
	}

	// firefly internal dateFormat := "2006-01-02T15:04:05-07:00"
	txnDate, err := time.Parse(time.DateOnly, t.Date)
	if err != nil {
		return time.Now(), errors.New(fmt.Sprintf("Could not parse date '%s'", t.Date))
	}

	if t.Description == "" {
		return txnDate, errors.New("description must be provided")
	}

	if t.SourceID == "" && t.SourceName == "" {
		return txnDate, errors.New("source_id or source_name must be provided")
	}

	if t.DestinationID == "" && t.DestinationName == "" {
		return txnDate, errors.New("destination_id or destination_name must be provided")
	}

	// If the user accidentally enters the same account for the source and
	// destination, we will have an expense and a revenue account with the same
	// name and won't be able to create new transactions for either one!
	if (t.DestinationID != "" && t.DestinationID == t.SourceID) || (t.DestinationName != "" && t.DestinationName == t.SourceName) {
		return txnDate, errors.New(fmt.Sprintf("source and destination accounts cannot be the same (got '%s' for both!)", t.DestinationName))
	}

	if t.Amount.IsZero() {
		return txnDate, errors.New("amount must be provided")
	}

	// Determine the transaction type
	if t.Type == "" {
		t.Type = f.calcTxnType(t.SourceID, t.SourceName, t.DestinationID, t.DestinationName)
	}
	return txnDate, nil
}
