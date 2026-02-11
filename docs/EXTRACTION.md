# LLM Extraction Prompts & Grammars

## LLM Backend Options

### Ollama (Default)

Local inference using Ollama's `/api/generate` endpoint. Recommended models:

- **Mistral 7B** (Q4_K_M or Q5_K_M quantization) — best balance of speed and accuracy for structured extraction
- **llama3.1:8b** — alternative with strong instruction following

Settings:
- `temperature: 0.1` — deterministic extraction
- `format: "json"` — Ollama's JSON mode
- `stream: false` — complete response
- `num_predict: 512` — capped output for small JSON schemas

### Anthropic Claude API

Use Claude Haiku (or any model) for extraction when local LLM isn't available or when higher accuracy is needed. Configure via `llm.backend: anthropic` in config.

- Uses tool_use or JSON mode for structured output
- Model is configurable (default: `claude-haiku-4-20250514`)
- Requires `ANTHROPIC_API_KEY` environment variable
- Higher per-call cost but more reliable extraction

### OpenAI-Compatible

Any endpoint that speaks the OpenAI chat completions API (e.g., vLLM, text-generation-inference, LM Studio).

### Backend Interface

All backends implement:

```go
type LLMBackend interface {
    Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
    Name() string
}
```

## Classification Prompt (fast, title-only)

```
Classify this eBay listing into exactly one component type.

Title: {{title}}

Types: ram, drive, server, cpu, nic, other

Respond with only the type.
```

## RAM Extraction Prompt

```
Extract structured attributes from this eBay server RAM listing.
Respond ONLY with a JSON object matching the schema below.
If a field cannot be determined, use null.
For quantity, default to 1 unless the title/description explicitly indicates a lot or bundle.

Title: {{title}}
Item Specifics: {{item_specifics}}

Schema:
{
  "manufacturer": string | null,      // Samsung, Micron, SK Hynix, Kingston, etc.
  "part_number": string | null,       // exact MPN if visible
  "capacity_gb": integer | null,      // per stick, NOT total for lots
  "quantity": integer,                // number of sticks in listing
  "generation": string | null,        // DDR3, DDR4, DDR5
  "speed_mhz": integer | null,       // 2133, 2400, 2666, 2933, 3200
  "ecc": boolean | null,
  "registered": boolean | null,       // RDIMM=true, UDIMM=false, LRDIMM=true
  "form_factor": string | null,       // RDIMM, LRDIMM, UDIMM, SO-DIMM
  "rank": string | null,              // 1Rx4, 2Rx4, 1Rx8, 2Rx8, 4Rx4
  "voltage": string | null,           // 1.2V, 1.35V, 1.5V
  "condition": string,                // new, like_new, used_working, for_parts, unknown
  "compatible_servers": [string],     // if mentioned: R640, R740, DL380, etc.
  "confidence": float                 // 0.0–1.0 your confidence in this extraction
}

Examples:

Title: "Samsung 32GB 2Rx4 PC4-2666V-RB2-11 DDR4 ECC REG M393A4K40CB2-CTD"
{
  "manufacturer": "Samsung",
  "part_number": "M393A4K40CB2-CTD",
  "capacity_gb": 32,
  "quantity": 1,
  "generation": "DDR4",
  "speed_mhz": 2666,
  "ecc": true,
  "registered": true,
  "form_factor": "RDIMM",
  "rank": "2Rx4",
  "voltage": "1.2V",
  "condition": "used_working",
  "compatible_servers": [],
  "confidence": 0.95
}

Title: "LOT OF 8x 16GB PC4-2400T Hynix HMA82GR7AFR8N-UH DDR4 ECC REG Server Memory"
{
  "manufacturer": "SK Hynix",
  "part_number": "HMA82GR7AFR8N-UH",
  "capacity_gb": 16,
  "quantity": 8,
  "generation": "DDR4",
  "speed_mhz": 2400,
  "ecc": true,
  "registered": true,
  "form_factor": "RDIMM",
  "rank": null,
  "voltage": null,
  "condition": "used_working",
  "compatible_servers": [],
  "confidence": 0.93
}
```

## Drive Extraction Prompt

```
Extract structured attributes from this eBay server drive listing.
Respond ONLY with a JSON object. If a field cannot be determined, use null.

Title: {{title}}
Item Specifics: {{item_specifics}}

Schema:
{
  "manufacturer": string | null,
  "part_number": string | null,
  "capacity": string | null,          // normalized: "300GB", "1.2TB", "3.84TB"
  "capacity_bytes": integer | null,   // in bytes for sorting
  "quantity": integer,
  "interface": string | null,         // SAS, SATA, NVMe, U.2
  "form_factor": string | null,       // 2.5, 3.5
  "type": string | null,              // SSD, HDD
  "rpm": integer | null,              // 7200, 10000, 15000 (null for SSD)
  "endurance": string | null,         // DWPD or PBW if mentioned
  "encryption": boolean | null,       // SED/FIPS
  "carrier_included": boolean | null, // tray/caddy included
  "carrier_type": string | null,      // "Dell Gen14", "HP G8-G10", etc.
  "condition": string,
  "confidence": float
}
```

## Server Extraction Prompt

```
Extract structured attributes from this eBay server listing.
Respond ONLY with a JSON object. If a field cannot be determined, use null.

Title: {{title}}
Item Specifics: {{item_specifics}}
Description (first 500 chars): {{description}}

Schema:
{
  "manufacturer": string | null,      // Dell, HP/HPE, Supermicro, Lenovo
  "model": string | null,             // R640, R740xd, DL380 Gen10
  "generation": string | null,        // Gen14, Gen10, etc.
  "form_factor": string | null,       // 1U, 2U, 4U, tower
  "drive_bays": string | null,        // 8xSFF, 12xLFF, 24xSFF
  "drive_form_factor": string | null, // SFF (2.5"), LFF (3.5")
  "cpu_count": integer | null,
  "cpu_model": string | null,         // "Xeon Gold 6130" etc.
  "cpu_installed": boolean | null,    // are CPUs included?
  "ram_total_gb": integer | null,
  "ram_stick_count": integer | null,
  "ram_slots_total": integer | null,
  "drives_included": boolean | null,
  "drive_blanks_included": boolean | null,
  "raid_controller": string | null,   // H730p, P408i, etc.
  "power_supplies": integer | null,   // number of PSUs
  "idrac_license": string | null,     // Enterprise, Express, etc.
  "ilo_license": string | null,
  "rails_included": boolean | null,
  "bezel_included": boolean | null,
  "network_card": string | null,
  "quantity": integer,
  "condition": string,
  "boots_tested": boolean | null,     // "tested working", "boots to BIOS"
  "confidence": float
}
```

## CPU Extraction Prompt

```
Extract structured attributes from this eBay server CPU listing.
Respond ONLY with a JSON object. If a field cannot be determined, use null.

Title: {{title}}
Item Specifics: {{item_specifics}}

Schema:
{
  "manufacturer": string | null,      // Intel, AMD
  "family": string | null,            // Xeon, EPYC
  "series": string | null,            // Gold, Silver, Bronze, Platinum, Milan, Rome, Genoa
  "model": string | null,             // 6130, 6248R, 7763, 9554
  "generation": string | null,        // Skylake, Cascade Lake, Ice Lake, Sapphire Rapids, Milan, Rome, Genoa
  "cores": integer | null,            // physical core count
  "threads": integer | null,          // thread count
  "base_clock_ghz": float | null,     // base frequency
  "boost_clock_ghz": float | null,    // max turbo frequency
  "tdp_watts": integer | null,        // thermal design power
  "socket": string | null,            // LGA3647, LGA4189, SP3, SP5
  "l3_cache_mb": integer | null,      // L3 cache in MB
  "part_number": string | null,       // Intel S-spec (SR3B0) or OPN
  "quantity": integer,
  "condition": string,                // new, like_new, used_working, for_parts, unknown
  "matched_pair": boolean | null,     // listing says "matched pair" or "matching"
  "confidence": float
}

Examples:

Title: "Intel Xeon Gold 6130 SR3B0 2.1GHz 16-Core 22MB LGA3647 Server CPU Processor"
{
  "manufacturer": "Intel",
  "family": "Xeon",
  "series": "Gold",
  "model": "6130",
  "generation": "Skylake",
  "cores": 16,
  "threads": null,
  "base_clock_ghz": 2.1,
  "boost_clock_ghz": null,
  "tdp_watts": null,
  "socket": "LGA3647",
  "l3_cache_mb": 22,
  "part_number": "SR3B0",
  "quantity": 1,
  "condition": "used_working",
  "matched_pair": false,
  "confidence": 0.95
}

Title: "Matched Pair 2x AMD EPYC 7763 64-Core 2.45GHz SP3 280W Server CPU"
{
  "manufacturer": "AMD",
  "family": "EPYC",
  "series": null,
  "model": "7763",
  "generation": "Milan",
  "cores": 64,
  "threads": null,
  "base_clock_ghz": 2.45,
  "boost_clock_ghz": null,
  "tdp_watts": 280,
  "socket": "SP3",
  "l3_cache_mb": null,
  "part_number": null,
  "quantity": 2,
  "condition": "used_working",
  "matched_pair": true,
  "confidence": 0.92
}
```

## NIC Extraction Prompt

```
Extract structured attributes from this eBay server network card listing.
Respond ONLY with a JSON object. If a field cannot be determined, use null.

Title: {{title}}
Item Specifics: {{item_specifics}}

Schema:
{
  "manufacturer": string | null,      // Intel, Mellanox/NVIDIA, Broadcom, Chelsio, HPE, Dell
  "model": string | null,             // X710-DA2, ConnectX-4, BCM57810, T6225-CR
  "speed": string | null,             // 1GbE, 10GbE, 25GbE, 40GbE, 100GbE
  "port_count": integer | null,       // number of ports
  "port_type": string | null,         // SFP+, SFP28, QSFP+, QSFP28, RJ45, BaseT
  "interface": string | null,         // PCIe x8, PCIe x16, OCP 3.0, mezzanine
  "pcie_generation": string | null,   // Gen3, Gen4
  "firmware_protocol": string | null, // Ethernet, InfiniBand, FCoE, iSCSI, RoCE
  "part_number": string | null,
  "oem_part_number": string | null,   // Dell/HP specific part number if applicable
  "low_profile": boolean | null,      // half-height bracket
  "transceivers_included": boolean | null,  // SFP+/SFP28 modules included
  "quantity": integer,
  "condition": string,                // new, like_new, used_working, for_parts, unknown
  "confidence": float
}

Examples:

Title: "Intel X710-DA2 10GbE SFP+ Dual Port PCIe x8 Server Network Adapter"
{
  "manufacturer": "Intel",
  "model": "X710-DA2",
  "speed": "10GbE",
  "port_count": 2,
  "port_type": "SFP+",
  "interface": "PCIe x8",
  "pcie_generation": "Gen3",
  "firmware_protocol": "Ethernet",
  "part_number": null,
  "oem_part_number": null,
  "low_profile": null,
  "transceivers_included": null,
  "quantity": 1,
  "condition": "used_working",
  "confidence": 0.94
}

Title: "LOT 4x Mellanox ConnectX-4 Lx 25GbE SFP28 2-Port LP CX4121A MCX4121A-ACAT"
{
  "manufacturer": "Mellanox",
  "model": "ConnectX-4 Lx",
  "speed": "25GbE",
  "port_count": 2,
  "port_type": "SFP28",
  "interface": null,
  "pcie_generation": null,
  "firmware_protocol": "Ethernet",
  "part_number": "MCX4121A-ACAT",
  "oem_part_number": null,
  "low_profile": true,
  "transceivers_included": null,
  "quantity": 4,
  "condition": "used_working",
  "confidence": 0.93
}
```

## GBNF Grammar (RAM example)

For use with Ollama or llama.cpp's `--grammar` flag to guarantee valid JSON output:

```gbnf
root ::= "{" ws members ws "}"

members ::= pair ("," ws pair)*

pair ::= ws string ws ":" ws value ws

value ::= string | number | integer | boolean | null | array

string ::= "\"" ([^"\\] | "\\" .)* "\""
number ::= "-"? [0-9]+ ("." [0-9]+)?
integer ::= "-"? [0-9]+
boolean ::= "true" | "false"
null ::= "null"
array ::= "[" ws (value ("," ws value)*)? ws "]"

ws ::= [ \t\n\r]*
```

For tighter enforcement, a schema-specific grammar can lock down field names and types:

```gbnf
root ::= "{" ws
  "\"manufacturer\"" ws ":" ws (string | null) "," ws
  "\"part_number\"" ws ":" ws (string | null) "," ws
  "\"capacity_gb\"" ws ":" ws (integer | null) "," ws
  "\"quantity\"" ws ":" ws integer "," ws
  "\"generation\"" ws ":" ws (gen | null) "," ws
  "\"speed_mhz\"" ws ":" ws (integer | null) "," ws
  "\"ecc\"" ws ":" ws (boolean | null) "," ws
  "\"registered\"" ws ":" ws (boolean | null) "," ws
  "\"form_factor\"" ws ":" ws (ff | null) "," ws
  "\"rank\"" ws ":" ws (string | null) "," ws
  "\"voltage\"" ws ":" ws (string | null) "," ws
  "\"condition\"" ws ":" ws condition "," ws
  "\"compatible_servers\"" ws ":" ws strarray "," ws
  "\"confidence\"" ws ":" ws number ws
"}"

gen ::= "\"DDR3\"" | "\"DDR4\"" | "\"DDR5\""
ff ::= "\"RDIMM\"" | "\"LRDIMM\"" | "\"UDIMM\"" | "\"SO-DIMM\""
condition ::= "\"new\"" | "\"like_new\"" | "\"used_working\"" | "\"for_parts\"" | "\"unknown\""

string ::= "\"" [a-zA-Z0-9_ ./-]* "\""
integer ::= [0-9]+
number ::= "0." [0-9]+
boolean ::= "true" | "false"
null ::= "null"
strarray ::= "[" ws (string ("," ws string)*)? ws "]"
ws ::= [ \t\n\r]*
```

Note: When using the Anthropic Claude API backend, grammar enforcement is not used. Instead, Claude's tool_use feature or JSON mode provides structured output. The extraction logic handles both paths transparently.

## Condition Normalization

Map eBay's condition strings and LLM-extracted condition to the normalized enum:

| Raw Values                                                               | Normalized     |
|--------------------------------------------------------------------------|----------------|
| New, Brand New, Factory Sealed                                           | `new`          |
| Open Box, Manufacturer Refurbished                                       | `like_new`     |
| Used, Pre-Owned, Seller Refurbished, Pulled from Working, Tested Working | `used_working` |
| For Parts, Not Working, Parts Only, As-Is                                | `for_parts`    |
| Anything else                                                            | `unknown`      |

Prefer eBay's structured condition over LLM extraction when available. Use LLM as fallback.

## Product Key Generation Logic

```go
func ProductKey(componentType string, attrs map[string]any) string {
    switch componentType {
    case "ram":
        return fmt.Sprintf("ram:%s:%s:%dgb:%d",
            normalizeStr(attrs["generation"]),
            ramType(attrs), // ecc_reg, ecc_unbuf, non_ecc
            attrInt(attrs, "capacity_gb"),
            attrInt(attrs, "speed_mhz"),
        )
    case "drive":
        return fmt.Sprintf("drive:%s:%s:%s:%s",
            normalizeStr(attrs["interface"]),
            normalizeStr(attrs["form_factor"]),
            normalizeStr(attrs["capacity"]),
            driveType(attrs), // ssd, 7k2, 10k, 15k
        )
    case "server":
        return fmt.Sprintf("server:%s:%s:%s",
            normalizeStr(attrs["manufacturer"]),
            normalizeStr(attrs["model"]),
            normalizeStr(attrs["drive_form_factor"]),
        )
    case "cpu":
        return fmt.Sprintf("cpu:%s:%s:%s",
            normalizeStr(attrs["manufacturer"]),
            normalizeStr(attrs["family"]),
            normalizeStr(attrs["model"]),
        )
    case "nic":
        return fmt.Sprintf("nic:%s:%dp:%s",
            normalizeStr(attrs["speed"]),
            attrInt(attrs, "port_count"),
            normalizeStr(attrs["port_type"]),
        )
    default:
        return fmt.Sprintf("other:%s", componentType)
    }
}
```

## Extraction Validation Rules

After parsing the JSON response from any backend, validate per component type:

### RAM
- `capacity_gb`: 1–1024 (required)
- `speed_mhz`: 800–8400 (optional)
- `generation`: one of DDR3, DDR4, DDR5 (required)
- `quantity`: >= 1

### Drive
- `capacity`: non-empty string (required)
- `interface`: one of SAS, SATA, NVMe, U.2 (required)
- `form_factor`: one of 2.5, 3.5 (optional)
- `type`: one of SSD, HDD (optional)
- `rpm`: null for SSD, 7200/10000/15000 for HDD (optional)

### Server
- `manufacturer`: non-empty (required)
- `model`: non-empty (required)
- `form_factor`: one of 1U, 2U, 4U, tower (optional)

### CPU
- `manufacturer`: one of Intel, AMD (required)
- `family`: one of Xeon, EPYC (required)
- `model`: non-empty (required)
- `cores`: 1–256 (optional)
- `base_clock_ghz`: 0.5–6.0 (optional)
- `tdp_watts`: 10–500 (optional)

### NIC
- `speed`: one of 1GbE, 10GbE, 25GbE, 40GbE, 100GbE (required)
- `port_count`: 1–8 (required)
- `port_type`: one of SFP+, SFP28, QSFP+, QSFP28, RJ45, BaseT (optional)

### All Types
- `condition`: one of new, like_new, used_working, for_parts, unknown
- `confidence`: 0.0–1.0
- `quantity`: >= 1

If validation fails, log the raw response and set `extraction_confidence = 0.0`. The listing is stored but excluded from scoring.
