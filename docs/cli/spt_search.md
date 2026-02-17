## spt search

Search eBay for server hardware listings

### Synopsis

Sends a search request to the API server and displays raw eBay results.

```
spt search <query> [flags]
```

### Examples

```
  spt search "DDR4 ECC 32GB RDIMM"
  spt search "Dell PowerEdge R630" --limit 25
```

### Options

```
  -h, --help        help for search
      --limit int   maximum number of results (default 10)
```

### Options inherited from parent commands

```
      --config string   config file (default $HOME/.spt.yaml)
      --output string   output format (table, json) (default "table")
      --server string   API server URL (default "http://localhost:8080")
```

### SEE ALSO

* [spt](spt.md)	 - CLI client for Server Price Tracker

