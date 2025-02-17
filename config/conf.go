package config

import (
	"github.com/go-yaml/yaml"
	"github.com/rs/zerolog/log"
	"os"
)

type MasterConfig struct {
	Accounts                  map[string]string            `yaml:"accounts"`
	NonAssetAccounts          map[string]string            `yaml:"non_asset_accounts"`
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

func InitConfig(path string) *MasterConfig {
	init := MasterConfig{}
	init.getConf(path)
	return &init
}
func (c *MasterConfig) getConf(file string) *MasterConfig {
	yamlFile, _ := os.ReadFile(file)
	err := yaml.Unmarshal(yamlFile, c)
	if err != nil {
		log.Fatal().Err(err).Msgf("%v ", err)
	}
	return c
}
