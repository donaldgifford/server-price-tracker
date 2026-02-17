## spt ingest

Trigger manual ingestion

### Synopsis

Triggers the ingestion pipeline to poll eBay for all enabled watches.

```
spt ingest [flags]
```

### Examples

```
  spt ingest
```

### Options

```
  -h, --help   help for ingest
```

### Options inherited from parent commands

```
      --config string   config file (default $HOME/.spt.yaml)
      --output string   output format (table, json) (default "table")
      --server string   API server URL (default "http://localhost:8080")
```

### SEE ALSO

- [spt](spt.md) - CLI client for Server Price Tracker
