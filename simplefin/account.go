package simplefin

import (
	"encoding/json"
	"fmt"
	"github.com/shopspring/decimal"
	"net/http"
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

type organization struct {
	Domain  string `json:"domain"`
	SfinURL string `json:"sfin-url"`
	Name    string `json:"name"`
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
	Org              organization    `json:"org"`
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
	APICalls float64 = 0
)

func New(accessUrl string) *Simplefin {
	return &Simplefin{
		url: accessUrl,
		filter: Filter{
			Pending: false,
		},
	}
}

func (f *Simplefin) SetFilter(newFilter Filter) { f.filter = newFilter }

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

func (f *Simplefin) Accounts() (AccountsResponse, error) {
	var accs AccountsResponse

	/*JSONFile, err := os.Open("accounts.json")
	byteValue, _ := io.ReadAll(JSONFile)
	err = json.Unmarshal(byteValue, &accs)
	if err != nil {
		return AccountsResponse{}, err
	}
	return accs, nil*/

	APICalls++
	postURL := f.url + "/accounts" + f.ToQuery()

	res, err := http.Get(postURL)
	if err != nil {
		return AccountsResponse{}, err
	}
	if res.StatusCode != http.StatusOK {
		return AccountsResponse{}, fmt.Errorf("%s - %v", res.Status, res.StatusCode)
	}

	err = json.NewDecoder(res.Body).Decode(&accs)
	if err != nil {
		return AccountsResponse{}, err
	}

	/*jsonString, _ := json.Marshal(accs)
	os.WriteFile("accounts.json", jsonString, os.ModePerm)*/

	return accs, nil
}
