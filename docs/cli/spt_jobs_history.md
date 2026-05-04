## spt jobs history

Show run history for a job

```
spt jobs history <job_name> [flags]
```

### Examples

```
  spt jobs history ingestion
  spt jobs history baseline_refresh --output json
```

### Options

```
  -h, --help   help for history
```

### Options inherited from parent commands

```
      --config string   config file (default $HOME/.spt.yaml)
      --output string   output format (table, json) (default "table")
      --server string   API server URL (default "http://localhost:8080")
```

### SEE ALSO

* [spt jobs](spt_jobs.md)	 - View scheduler job history

