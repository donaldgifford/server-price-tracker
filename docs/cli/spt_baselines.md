## spt baselines

Manage price baselines

### Synopsis

View and refresh price baselines used to compute deal scores. Baselines
aggregate historical price data by product key to determine whether a listing is
priced above or below market.

### Options

```
  -h, --help   help for baselines
```

### Options inherited from parent commands

```
      --config string   config file (default $HOME/.spt.yaml)
      --output string   output format (table, json) (default "table")
      --server string   API server URL (default "http://localhost:8080")
```

### SEE ALSO

- [spt](spt.md) - CLI client for Server Price Tracker
- [spt baselines get](spt_baselines_get.md) - Show baseline details
- [spt baselines list](spt_baselines_list.md) - List all baselines
- [spt baselines refresh](spt_baselines_refresh.md) - Trigger baseline refresh
