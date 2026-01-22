package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/forPelevin/gomoji"
	"github.com/helpcomp/firefly-iii-simplefin-importer/config"
	"github.com/helpcomp/firefly-iii-simplefin-importer/firefly"
	"github.com/helpcomp/firefly-iii-simplefin-importer/simplefin"
	"github.com/rs/zerolog/log"
	"github.com/sashabaranov/go-openai"
)

// ExtractCompanyAndCategory processes a transaction to extract the company and category details for classification.
// It uses predefined bypass rules and optionally integrates with OpenAI for enhanced categorization insights.
func ExtractCompanyAndCategory(ff *firefly.Firefly, c *config.MasterConfig, oai *openai.Client, transaction simplefin.Transactions) ExtractedData {
	var extracted = ExtractedData{
		Company:   defaultAccountName,
		Category:  "",
		Skip:      false,
		CompanyID: "",
	}

	var sbCategories, sbAccounts strings.Builder

	if transaction.Description == "" {
		return extracted
	}

	fireflyCategories, err := ff.CachedCategories()
	if err != nil {
		log.Error().Err(err).Msgf("Error getting cached categories - %v", err)
		return extracted
	}

	fireflyAccounts, err := ff.CachedAccounts()
	if err != nil {
		log.Error().Err(err).Msgf("Error getting cached accounts - %v", err)
		return extracted
	}

	// Loop through Account Names
	var expenseAccounts []string
	for _, aa := range fireflyAccounts.Accounts {
		if aa.Attributes.Type == "expense" {
			expenseAccounts = append(expenseAccounts, aa.Attributes.Name)
		}
	}
	sbAccounts.WriteString(strings.Join(expenseAccounts, ", "))

	// Loop through Categories
	var sbCatArray []string
	for _, cc := range fireflyCategories {
		sbCatArray = append(sbCatArray, cc.Name)
	}
	sbCategories.WriteString(strings.Join(sbCatArray, ", "))

	if len(expenseAccounts) == 0 || len(fireflyCategories) == 0 {
		log.Warn().Msg("No expense accounts or categories available for AI categorization")
		return extracted
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
	if cli.OpenAIAPIKey == "" && (cli.AzureAIAPIKey == "" || cli.AzureEndpoint == "") {
		log.Info().Msgf("No OpenAI API Key provided, using default")
		return extracted
	}

	// OpenAI / ChatGPT
	// Keeping for now to test new String Builder prompt
	// prompt := fmt.Sprintf("Given the following transaction %s Please respond with JSON using Merchant and Category. \"Merchant\" which is your best guess at the merchant the bank transaction stemmed from using the following list seperated by commas: %s . If a sutable merchant isn't found from the list you can choose your own. when the payment was made via a payment service like paypal only show the merchant name not the payment service used. \"Category\" a general business accounting category you would expect this sort of transaction to be categorized to from the following list seperated by commas: %s . Choose the best category that fits this transaction. Choose only one merchant and category. Do not respond in anything other than JSON, No English unless in JSON format.", transaction.Description, sbAccounts.String(), sbCategories.String())
	//  transaction.Description, sbAccounts.String(), sbCategories.String())
	var prompt strings.Builder
	prompt.WriteString("I want to categorize transactions on my bank account. Given the following transaction: ")
	prompt.WriteString(transaction.Description)
	prompt.WriteString("\n\n\"Merchant\" which is your best guess at the merchant the bank transaction stemmed from using the following list: ")
	prompt.WriteString(sbAccounts.String())
	prompt.WriteString("\nIf a suitable merchant isn't found from the list, you can choose your own. When the payment was made via a payment service like PayPal only show the merchant name, not the payment service used. \"Category\" a general business accounting category, Please choose a category that this transaction would fall under from the following list: ")
	prompt.WriteString(sbCategories.String())
	prompt.WriteString("\nChoose the best category that fits this transaction. Choose only one merchant and category. Please respond only in JSON, do not respond in anything other than JSON, No English unless in JSON format.")

	var modifiedResp string
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// GPT3Dot5TurboInstruct
	if cli.OpenAIModel == openai.GPT3Dot5TurboInstruct {
		req := openai.CompletionRequest{
			Model:     cli.OpenAIModel,
			Prompt:    prompt.String(),
			MaxTokens: 256,
		}
		resp, err := oai.CreateCompletion(ctx, req)
		if err != nil {
			log.Error().Err(err).Msgf("Error with ChatGPT/OpenAI : %v", err)
			return extracted
		}

		modifiedResp = resp.Choices[0].Text
	} else {
		// New
		resp, err := oai.CreateChatCompletion(
			context.Background(),
			openai.ChatCompletionRequest{
				Model: cli.OpenAIModel,
				Messages: []openai.ChatCompletionMessage{
					{
						Role:    openai.ChatMessageRoleAssistant,
						Content: prompt.String(),
					},
				},
			},
		)

		if err != nil {
			log.Error().Err(err).Msgf("Error with ChatGPT/OpenAI chat request")
			return extracted
		}

		if len(resp.Choices) != 1 {
			log.Error().Msgf("Unexpected number of choices %v", resp.Choices)
			return extracted
		}

		modifiedResp = resp.Choices[0].Message.Content
	}

	// Split the text by semicolon to get Company and Category
	var rsp OpenAIResponse

	// Some ChatGPT models send us ```JSON {}``` instead of just JSON, so we have to parse it.
	if strings.Contains(modifiedResp, "```") {
		modifiedResp = strings.TrimPrefix(modifiedResp, "```json")
		modifiedResp = strings.TrimPrefix(modifiedResp, "```")
		modifiedResp = strings.TrimSuffix(modifiedResp, "```")
		modifiedResp = strings.TrimSpace(modifiedResp)
	}

	// Try to unmarshal the response into the rsp (OpenAIResponse)
	err = json.Unmarshal([]byte(modifiedResp), &rsp)
	if err != nil {
		log.Warn().Err(err).Msgf("ChatGPT responded with invalid JSON response.")
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
