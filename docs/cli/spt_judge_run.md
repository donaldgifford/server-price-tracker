## spt judge run

Run one tick of the LLM-as-judge worker

### Synopsis

Triggers one synchronous run of the LLM-as-judge worker against the configured server. Prints the number of alerts judged plus a budget-exhausted flag when the daily USD cap halts the run early.

```
spt judge run [flags]
```

### Examples

```
  spt judge run
```

### Options

```
  -h, --help   help for run
```

### Options inherited from parent commands

```
      --config string   config file (default $HOME/.spt.yaml)
      --output string   output format (table, json) (default "table")
      --server string   API server URL (default "http://localhost:8080")
```

### SEE ALSO

* [spt judge](spt_judge.md)	 - LLM-as-judge worker controls

