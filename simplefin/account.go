package simplefin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

type Simplefin struct {
	url       string
	filter    Filter
	CacheOnly bool
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

type AccountJson struct {
	Data       AccountsResponse
	ParsedTime time.Time
}

// New initializes and returns a new Simplefin instance with the provided accessUrl and cacheOnly settings.
func New(accessUrl string, cacheOnly bool) *Simplefin {
	if cacheOnly {
		log.Debug().Msg("Running in Cache Only Mode")
	}

	return &Simplefin{
		url: accessUrl,
		filter: Filter{
			Pending: false,
		},
		CacheOnly: cacheOnly,
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
	var accountsResposneData AccountJson

	// Debug -- Read From Cache
	// This is to prevent a bunch of API calls to SimpleFin when I'm debugging.
	// And since I forget to disable caching, might as well add a check
	if f.CacheOnly {
		JSONFile, _ := os.Open("accounts.json")
		byteValue, _ := io.ReadAll(JSONFile)

		defer func(JSONFile *os.File) {
			err := JSONFile.Close()
			if err != nil {
				log.Err(err).Msg("Error closing accounts.json!")
			}
		}(JSONFile)

		err := json.Unmarshal(byteValue, &accountsResposneData)

		if err == nil {
			now := time.Now()
			dur := now.Sub(accountsResposneData.ParsedTime)

			if dur.Hours() < 48 {
				validFor := 48 - dur.Hours()
				log.Debug().Msg("[Cache Only Mode] : Cache is valid for " + strconv.FormatFloat(validFor, 'f', 0, 64) + " hours.")
				return accountsResposneData.Data, nil
			} else {
				log.Debug().Msg("Cache expired, fetching from API.")
			}
		} else {
			log.Debug().Msg("Missing accounts.json, fetching from API.")
		}
	}

	postURL := f.url + "/accounts" + f.ToQuery()

	res, err := http.Get(postURL)
	if err != nil {
		return AccountsResponse{}, err
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(res.Body)

	if res.StatusCode != http.StatusOK {
		return AccountsResponse{}, fmt.Errorf("invalid HTTP Status Code %d", res.StatusCode)
	}

	err = json.NewDecoder(res.Body).Decode(&accountsResponse)
	if err != nil {
		return AccountsResponse{}, err
	}

	// Debug Only - Write to accounts.json
	if f.CacheOnly {
		jsonString, _ := json.Marshal(AccountJson{
			Data:       accountsResponse,
			ParsedTime: time.Now(),
		})
		err = os.WriteFile("accounts.json", jsonString, os.ModePerm)
		if err != nil {
			log.Err(err).Msg("Failed to write accounts.json!")
		}
	}

	return accountsResponse, nil
}
