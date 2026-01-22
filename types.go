package main

// Shared constants
const defaultAccountName = "(no name)"

// OpenAIResponse represents the JSON response from OpenAI API
type OpenAIResponse struct {
	Merchant string `json:"Merchant"`
	Category string `json:"Category"`
}

// ExtractedData holds the extracted company and category information from a transaction
type ExtractedData struct {
	Company   string
	Category  string
	Skip      bool
	CompanyID string
}
