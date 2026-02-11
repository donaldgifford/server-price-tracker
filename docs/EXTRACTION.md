# LLM Extraction Prompts & Grammars

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

## GBNF Grammar (RAM example)

For use with llama.cpp's `--grammar` flag to guarantee valid JSON output:

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
    default:
        return fmt.Sprintf("other:%s", componentType)
    }
}
```

## Scoring Function

```go
func ScoreListing(listing Listing, baseline *PriceBaseline) int {
    scores := map[string]float64{}

    // Price percentile (40%)
    if baseline != nil && baseline.SampleCount >= 10 {
        scores["price"] = pricePercentileScore(listing.UnitPrice(), baseline)
    } else {
        scores["price"] = 50 // neutral if no baseline
    }

    // Seller trust (20%)
    scores["seller"] = sellerScore(listing)

    // Condition (15%)
    scores["condition"] = conditionScore(listing.ConditionNorm)

    // Quantity value (10%) - lots with good per-unit price score higher
    scores["quantity"] = quantityScore(listing)

    // Listing quality (10%)
    scores["quality"] = qualityScore(listing)

    // Time pressure (5%)
    scores["time"] = timeScore(listing)

    weights := map[string]float64{
        "price": 0.40, "seller": 0.20, "condition": 0.15,
        "quantity": 0.10, "quality": 0.10, "time": 0.05,
    }

    total := 0.0
    for k, w := range weights {
        total += scores[k] * w
    }

    return int(math.Round(total))
}

func pricePercentileScore(unitPrice float64, b *PriceBaseline) float64 {
    switch {
    case unitPrice <= b.P10: return 100
    case unitPrice <= b.P25: return lerp(unitPrice, b.P10, b.P25, 100, 85)
    case unitPrice <= b.P50: return lerp(unitPrice, b.P25, b.P50, 85, 50)
    case unitPrice <= b.P75: return lerp(unitPrice, b.P50, b.P75, 50, 25)
    case unitPrice <= b.P90: return lerp(unitPrice, b.P75, b.P90, 25, 0)
    default: return 0
    }
}
```
