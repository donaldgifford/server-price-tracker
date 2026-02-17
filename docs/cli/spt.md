## spt

CLI client for Server Price Tracker

### Synopsis

spt is a command-line client for the Server Price Tracker API. It lets you
manage watches, query listings, trigger ingestion, and run extractions from the
terminal.

### Options

```
      --config string   config file (default $HOME/.spt.yaml)
  -h, --help            help for spt
      --output string   output format (table, json) (default "table")
      --server string   API server URL (default "http://localhost:8080")
```

### SEE ALSO

- [spt baselines](spt_baselines.md) - Manage price baselines
- [spt extract](spt_extract.md) - Extract structured attributes from a listing
  title
- [spt ingest](spt_ingest.md) - Trigger manual ingestion
- [spt listings](spt_listings.md) - Query listings
- [spt rescore](spt_rescore.md) - Rescore all listings
- [spt search](spt_search.md) - Search eBay for server hardware listings
- [spt watches](spt_watches.md) - Manage watches
