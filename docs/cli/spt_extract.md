## spt extract

Extract structured attributes from a listing title

### Synopsis

Sends a title to the API server for LLM-based classification and attribute
extraction.

```
spt extract <title> [flags]
```

### Examples

```
  spt extract "Samsung 32GB DDR4 2666MHz ECC REG M393A4K40CB2-CTD"
  spt extract "Dell PowerEdge R630 2x Xeon E5-2680 v4 128GB 8x 2.5in"
```

### Options

```
  -h, --help   help for extract
```

### Options inherited from parent commands

```
      --config string   config file (default $HOME/.spt.yaml)
      --output string   output format (table, json) (default "table")
      --server string   API server URL (default "http://localhost:8080")
```

### SEE ALSO

- [spt](spt.md) - CLI client for Server Price Tracker
