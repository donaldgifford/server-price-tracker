## spt watches update

Update an existing watch

### Synopsis

Update fields on an existing watch. Only flags explicitly passed are
changed; everything else is preserved by fetching the current state
and PUT-ing the merged result.

Filter semantics:
  --filter        replaces the entire filter block
  --add-filter    merges attribute filters into the existing map
  --clear-filters empties the filter block
At most one of these three may be passed in a single invocation.

```
spt watches update <id> [flags]
```

### Examples

```
  # Tighten the score threshold without touching anything else
  spt watches update abc123 --threshold 80

  # Add a capacity_gb constraint without dropping existing filters
  spt watches update abc123 --add-filter "attr:capacity_gb=eq:32"

  # Replace the entire filter block
  spt watches update abc123 --filter "attr:capacity_gb=eq:64" --filter "price_max=500"

  # Clear all filters
  spt watches update abc123 --clear-filters
```

### Options

```
      --add-filter stringArray   merge attribute filters into the existing map (attr:key=value, repeatable)
      --category string          category id
      --clear-filters            clear all filters on the watch
      --enabled                  enable or disable the watch
      --filter stringArray       replace the entire filter block (key=value, repeatable)
  -h, --help                     help for update
      --name string              watch name
      --query string             eBay search query
      --threshold int            score threshold for alerts
      --type string              component type (ram, drive, server, cpu, nic, gpu, workstation, desktop, other)
```

### Options inherited from parent commands

```
      --config string   config file (default $HOME/.spt.yaml)
      --output string   output format (table, json) (default "table")
      --server string   API server URL (default "http://localhost:8080")
```

### SEE ALSO

* [spt watches](spt_watches.md)	 - Manage watches

