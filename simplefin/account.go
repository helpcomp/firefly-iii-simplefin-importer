package simplefin

import (
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
	"io"
	"net/http"
	"os"
)

type Simplefin struct {
	url    string
	filter Filter
}

type Filter struct {
	StartDate int64
	EndDate   int64
	Pending   bool
}

type AccountsResponse struct {
	Errors   []string   `json:"errors"`
	Accounts []Accounts `json:"accounts"`
}

type Transactions struct {
	ID           string          `json:"id"`
	Posted       int64           `json:"posted"`
	TransactedAt int64           `json:"transacted_at,omitempty"` // "2018-09-17T12:46:47+01:00"
	Amount       decimal.Decimal `json:"amount"`
	Description  string          `json:"description"`
	Pending      bool            `json:"pending"`
	Extra        []string        `json:"extra"`
}
type Accounts struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Currency         string          `json:"currency"`
	Balance          decimal.Decimal `json:"balance"`
	AvailableBalance decimal.Decimal `json:"available-balance"`
	BalanceDate      int64           `json:"balance-date"`
	Transactions     []Transactions  `json:"transactions"`
	Extra            []string        `json:"extra"`
}

var (
	APICalls  float64 = 0
	CacheOnly         = false
)

// New initializes and returns a new Simplefin instance with the provided accessUrl and cacheOnly settings.
func New(accessUrl string, cacheOnly bool) *Simplefin {
	CacheOnly = cacheOnly
	if CacheOnly {
		log.Debug().Msg("Running in Cache Only Mode")
	}

	return &Simplefin{
		url: accessUrl,
		filter: Filter{
			Pending: false,
		},
	}
}

// SetFilter updates the filter property of the Simplefin instance with the provided Filter.
func (f *Simplefin) SetFilter(newFilter Filter) { f.filter = newFilter }

// ToQuery constructs a query string based on the filter fields of the Simplefin instance.
func (f *Simplefin) ToQuery() string {
	appendQuery := "?"
	if f.filter.StartDate > 0 {
		appendQuery += fmt.Sprintf("start-date=%d&", f.filter.StartDate)
	}
	if f.filter.EndDate > 0 {
		appendQuery += fmt.Sprintf("end-date=%d&", f.filter.EndDate)
	}
	if f.filter.Pending {
		appendQuery += fmt.Sprintf("pending=%v&", f.filter.Pending)
	}
	appendQuery = appendQuery[:len(appendQuery)-1]

	return appendQuery
}

// Accounts fetches account information from the Simplefin API or cache, returning an AccountsResponse or an error.
func (f *Simplefin) Accounts() (AccountsResponse, error) {
	var accountsResponse AccountsResponse

	// Debug -- Read From Cache
	// This is to prevent a bunch of API calls to SimpleFin when I'm debugging.
	// And since I forget to disable caching, might as well add a check
	if CacheOnly {
		JSONFile, _ := os.Open("accounts.json")
		byteValue, _ := io.ReadAll(JSONFile)

		err := json.Unmarshal(byteValue, &accountsResponse)
		if err == nil {
			return accountsResponse, nil
		}

		log.Debug().Msg("Missing accounts.json, fetching from API.")
	}

	APICalls++
	postURL := f.url + "/accounts" + f.ToQuery()

	res, err := http.Get(postURL)
	if err != nil {
		return AccountsResponse{}, err
	}
	if res.StatusCode != http.StatusOK {
		return AccountsResponse{}, fmt.Errorf("%s - %v", res.Status, res.StatusCode)
	}

	err = json.NewDecoder(res.Body).Decode(&accountsResponse)
	if err != nil {
		return AccountsResponse{}, err
	}

	// Debug Only - Write to accounts.json
	if CacheOnly {
		jsonString, _ := json.Marshal(accountsResponse)
		err = os.WriteFile("accounts.json", jsonString, os.ModePerm)
		if err != nil {
			log.Err(err).Msg("Failed to write accounts.json!")
		}
	}

	return accountsResponse, nil
}
