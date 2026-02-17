## spt listings list

List listings with optional filters

### Synopsis

List ingested listings with optional filters for component type, product key,
score range, and sorting.

```
spt listings list [flags]
```

### Examples

```
  # List all listings
  spt listings list

  # Filter by component type and minimum score
  spt listings list --type ram --min-score 70

  # Sort by price with pagination
  spt listings list --order-by price --limit 20 --offset 40

  # Filter by product key
  spt listings list --product-key "ram:ddr4:ecc_reg:32gb:2666"
```

### Options

```
  -h, --help                 help for list
      --limit int            number of results (default 50)
      --max-score int        maximum score filter
      --min-score int        minimum score filter
      --offset int           result offset
      --order-by string      sort order (score, price, first_seen_at)
      --product-key string   product key filter
      --type string          component type filter
```

### Options inherited from parent commands

```
      --config string   config file (default $HOME/.spt.yaml)
      --output string   output format (table, json) (default "table")
      --server string   API server URL (default "http://localhost:8080")
```

### SEE ALSO

- [spt listings](spt_listings.md) - Query listings
