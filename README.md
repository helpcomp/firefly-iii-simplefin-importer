# Firefly III Simplefin Importer

Use [Firefly III](https://github.com/firefly-iii/firefly-iii) to store transactions, and [Simplefin](https://beta-bridge.simplefin.org/) to import transaction data. Also takes advantage of OpenAI to categorize transactions.

## Credits

[lychnos](https://github.com/davidschlachter/lychnos) - For the Firefly III Go Backend, and the original source

[firefly_iii_exporter](https://github.com/kinduff/firefly_iii_exporter) - For the multithreaded Prometheus idea

[go-openai](https://github.com/sashabaranov/go-openai) - For OpenAI support in Go

[Firefly III](https://github.com/firefly-iii/firefly-iii) - For providing a free, open-source personal finance manager



## Setup
You will need a Firefly III with a personal access token, a SimpleFin Access URL.

Example `config.yml`:

```
---
  config:
    enable_reconciliation: false  # Automatic Reconciliation
    loopback_duration: 10d        # How many days of transactions to pull from Simplefin
    non_asset_accounts:
      100: reconciliation
      101: withdrawal
  # Manually categorizes transactions, and bypasses OpenAI Auto-Categorization
  transactionBypass:
    - ONLINE PURCHASE:
        company: "GitHub"
        category: "Shopping"
        source_account: "1"
        destination_account: "25"
        type: "transfer"
    - MONEY TRANSFER:
          skip: true
  # Accounts: Simplefin Account ID: Firefly Asset ID
  accounts:
    ACT-00000-0000-0000-0000-0000000000: 1
```
