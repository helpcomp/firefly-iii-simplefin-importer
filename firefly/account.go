package firefly

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/helpcomp/firefly-iii-simplefin-importer/httperror"
	"github.com/shopspring/decimal"
	"log"
	"net/http"
	"regexp"
)

type accountsResponse struct {
	Data  []Account `json:"data"`
	Meta  meta      `json:"meta"`
	Links links     `json:"links"`
}

type Account struct {
	ID         string            `json:"id"`
	Attributes AccountAttributes `json:"attributes"`
}

type AccountAttributes struct {
	Active         bool            `json:"active"`
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	CurrentBalance decimal.Decimal `json:"current_balance"`
}

func (f *Firefly) HandleAccount(w http.ResponseWriter, req *http.Request) {
	log.Printf("%s %s", req.Method, req.RequestURI)
	switch req.Method {
	case "GET":
		hasID := regexp.MustCompile(`/[0-9]+$`)
		if hasID.MatchString(req.URL.Path) {
			// f.fetchAccount(w, req)
		} else {
			f.listAccounts(w, req)
		}
	default:
		w.WriteHeader(http.StatusNotImplemented)
		fmt.Fprintf(w, "Unsupported method %s", req.Method)
	}
}

func (f *Firefly) listAccounts(w http.ResponseWriter, req *http.Request) {
	accounts, err := f.CachedAccounts()
	if err != nil {
		httperror.Send(w, req, http.StatusInternalServerError, fmt.Sprintf("Could not list accounts: %s", err))
		return
	}

	// If a type parameter was provided, filter the returned accounts
	acctTypes, ok := req.URL.Query()["type"]
	if ok && len(acctTypes) > 0 {
		var filteredAccounts []Account
		for _, t := range acctTypes {
			for _, a := range accounts {
				if a.Attributes.Type != t {
					continue
				}
				filteredAccounts = append(filteredAccounts, a)
			}
		}
		accounts = filteredAccounts
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(accounts)
}

func (f *Firefly) ListAccounts(accountType string) ([]Account, error) {
	const path = "/api/v1/accounts"

	var (
		results []Account
	)

	if accountType == "" {
		accountType = "all"
	}
	page := 1

	for more := true; more; page++ {
		var accs accountsResponse
		params := fmt.Sprintf("?type=%s&page=%d", accountType, page)
		req, _ := http.NewRequest("GET", f.url+path+params, nil)
		req.Header.Add("Authorization", "Bearer "+f.token)

		resp, err := f.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Accounts: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("got status %d", resp.StatusCode)
		}

		err = json.NewDecoder(resp.Body).Decode(&accs)
		if err != nil {
			return nil, err
		}
		results = append(results, accs.Data...)

		more = accs.Meta.Pagination.CurrentPage < accs.Meta.Pagination.TotalPages
		resp.Body.Close()
	}

	return results, nil
}

func (f *Firefly) ListAccountTransactions(accountID string) (TxnsResponse, error) {
	const path = "/api/v1/accounts"
	var err error

	if accountID == "" {
		return TxnsResponse{}, errors.New("missing Account ID")
	}

	params := fmt.Sprintf("/%s/transactions", accountID)
	req, _ := http.NewRequest("GET", f.url+path+params, nil)
	req.Header.Add("Authorization", "Bearer "+f.token)
	resp, err := f.client.Do(req)
	if err != nil {
		return TxnsResponse{}, fmt.Errorf("failed to fetch Accounts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return TxnsResponse{}, fmt.Errorf("got status %d", resp.StatusCode)
	}

	var accs TxnsResponse

	err = json.NewDecoder(resp.Body).Decode(&accs)
	if err != nil {
		return TxnsResponse{}, err
	}

	return accs, err
}
