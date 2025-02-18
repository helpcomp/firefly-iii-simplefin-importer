# Firefly III Simplefin Importer

Use [Firefly III](https://github.com/firefly-iii/firefly-iii) to store transactions, and [Simplefin](https://beta-bridge.simplefin.org/) to import transaction data. Also takes advantage of OpenAI to categorize transactions.

## Credits

[lychnos](https://github.com/davidschlachter/lychnos) - For the Firefly III Go Backend, and the original source

[firefly_iii_exporter](https://github.com/kinduff/firefly_iii_exporter) - For the multithreaded Prometheus idea

[go-openai](https://github.com/sashabaranov/go-openai) - For OpenAI support in Go

[Firefly III](https://github.com/firefly-iii/firefly-iii) - For providing a free, open-source personal finance manager



## Setup
Please follow https://github.com/helpcomp/firefly-iii-simplefin-importer/wiki/Getting-Started

You will need a Firefly III with a personal access token, and a SimpleFin Access URL.

Example `config.yml`:

```
---
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

Example `docker-compose.yml`:
```
services:     
  simplefinbridgeexporter:
    build: https://github.com/helpcomp/firefly-iii-simplefin-importer.git
    # env_file: .env  # You may either use environment, or env_File
    environment:
      - FIREFLY_TOKEN=
      - SIMPLEFIN_ACCESS_URL=
      - FIREFLY_URL=http://firefly:8080
      - OPENAI_API_KEY=
    volumes:
      - ./config.yml:/config.yml
    ports:
      - 9717:9717
    restart: always
```
