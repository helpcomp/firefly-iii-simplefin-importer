package config

import (
	"github.com/go-yaml/yaml"
	"log"
	"os"
)

type openAI struct {
	APIKey string `yaml:"key"`
}

type appConfig struct {
	EnableReconciliation bool              `yaml:"enable_reconciliation"`
	LoopbackDuration     string            `yaml:"loopback_duration"`
	NonAssetAccounts     map[string]string `yaml:"non_asset_accounts"`
}

type MasterConfig struct {
	Accounts                  map[string]string            `yaml:"accounts"`
	AppConfig                 appConfig                    `yaml:"config"`
	OpenAI                    openAI                       `yaml:"openai"`
	TransactionBypassResponse []map[string]TransactionInfo `yaml:"transactionBypass"`
}

type NonAssetAccountInfo struct {
	Type string `yaml:"type"`
}
type TransactionInfo struct {
	Company            string `yaml:"company"`
	Category           string `yaml:"category"`
	AssetID            string `yaml:"assetID,omitempty"`
	SourceAccount      string `yaml:"source_account,omitempty"`
	DestinationAccount string `yaml:"destination_account,omitempty"`
	Type               string `yaml:"type,omitempty"`
	Skip               bool   `yaml:"skip,omitempty"`
}
type TransactionBypassResp struct {
	Accounts map[string]TransactionInfo `yaml:"transactionBypass"`
}

func InitConfig() *MasterConfig {
	init := MasterConfig{}
	init.getConf("config.yml")
	return &init
}
func (c *MasterConfig) getConf(file string) *MasterConfig {

	yamlFile, err := os.ReadFile(file)
	if err != nil {
		log.Printf("yamlFile.Get err   #%v ", err)
	}
	err = yaml.Unmarshal(yamlFile, c)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}

	return c
}
