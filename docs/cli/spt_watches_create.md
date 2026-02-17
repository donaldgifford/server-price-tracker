## spt watches create

Create a new watch

### Synopsis

Create a new watch that defines an eBay search query, component type, score
threshold, and optional filters. The watch will be enabled by default and start
matching listings on the next ingestion cycle.

```
spt watches create [flags]
```

### Examples

```
  # Create a basic RAM watch
  spt watches create --name "DDR4 ECC 32GB" --query "DDR4 ECC 32GB RDIMM" --type ram

  # Create a watch with a custom threshold and filters
  spt watches create --name "Dell R630" --query "Dell PowerEdge R630" \
    --type server --threshold 80 \
    --filter "min_price=100" --filter "max_price=500"
```

### Options

```
      --filter stringArray   filters (key=value)
  -h, --help                 help for create
      --name string          watch name
      --query string         eBay search query
      --threshold int        score threshold for alerts (default 75)
      --type string          component type (ram, drive, server, cpu, nic)
```

### Options inherited from parent commands

```
      --config string   config file (default $HOME/.spt.yaml)
      --output string   output format (table, json) (default "table")
      --server string   API server URL (default "http://localhost:8080")
```

### SEE ALSO

- [spt watches](spt_watches.md) - Manage watches
