## spt reextract

Re-extract listings with incomplete data

### Synopsis

Re-runs LLM extraction on listings with quality issues
(e.g., missing RAM speed from PC module numbers).

```
spt reextract [flags]
```

### Examples

```
  spt reextract
  spt reextract --type ram
  spt reextract --type ram --limit 50
```

### Options

```
  -h, --help          help for reextract
      --limit int     max listings to process (default 100)
      --type string   component type filter (e.g., ram, drive, cpu)
```

### Options inherited from parent commands

```
      --config string   config file (default $HOME/.spt.yaml)
      --output string   output format (table, json) (default "table")
      --server string   API server URL (default "http://localhost:8080")
```

### SEE ALSO

* [spt](spt.md)  - CLI client for Server Price Tracker

