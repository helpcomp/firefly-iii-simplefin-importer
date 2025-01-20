# Firefly III Simplefin Importer

Use [Firefly III](https://github.com/firefly-iii/firefly-iii) to store transactions, and [Simplefin](https://beta-bridge.simplefin.org/) to import transaction data. Also takes advantage of OpenAI to categorize transactions.

**This is NOT meant for production use and may not even work in your scenario. USE AT YOUR OWN RISK.**

## Credits

[lychnos](https://github.com/davidschlachter/lychnos) - For the Firefly III Go Backend

[firefly_iii_exporter](https://github.com/kinduff/firefly_iii_exporter) - For the multithreaded Prometheus idea

[go-openai](https://github.com/sashabaranov/go-openai) - For OpenAI support in Go

[Firefly III](https://github.com/firefly-iii/firefly-iii) - For providing a free, open-source personal finance manager



## Setup
You will need a Firefly III with a personal access token, a SimpleFin Access URL.

Example `config.yml`:

```
---
  config:
    enable_reconciliation: false
    loopback_duration: 10d
    non_asset_accounts:
      127: reconciliation
      128: reconciliation
      108: withdrawal
      109: withdrawal
      110: withdrawal
      111: withdrawal
      112: withdrawal
      113: withdrawal
      114: withdrawal
      115: withdrawal
      116: withdrawal
      208: withdrawal

  openai:
      key: <OPEN AI KEY>

  transactionBypass:
    - I AM A TRANSACTION NAME:
        company: "Spending"
        category: "Shopping"
        source_account: "1"
        destination_account: "25"
        type: "transfer"
  # Accounts
  # <SimpleFin Account ID>:<Firefly Asset ID>
  accounts:
    ACT-00000000-0000-0000-0000-00000000000: 100
```
