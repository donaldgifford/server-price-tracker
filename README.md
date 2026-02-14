# server price tracker

An API-first Go service that monitors eBay listings for server hardware,
extracts structured attributes via LLM (Ollama, Anthropic Claude, or compatible
backends), scores listings against historical baselines, and alerts on deals via
Discord webhooks. The CLI acts as a client to the API, and the API design
supports future integrations (Discord bot, web UI, Grafana dashboards).

## LLM Extraction Schema

The extraction pipeline classifies each eBay listing into a component type, then
extracts structured attributes using component-specific prompts. Validation
enforces required fields and enum values. Fields marked **required** will fail
validation if the LLM returns `null`.

### Common Fields (all component types)

| Field | Type | Required | Values |
|-------|------|----------|--------|
| `condition` | enum | yes | `new`, `like_new`, `used_working`, `for_parts`, `unknown` |
| `confidence` | float | yes | 0.0 - 1.0 |
| `quantity` | integer | no | >= 1 (default 1) |

### RAM

| Field | Type | Required | Values |
|-------|------|----------|--------|
| `manufacturer` | string | no | |
| `part_number` | string | no | |
| `capacity_gb` | integer | yes | 1 - 1024 |
| `generation` | enum | yes | `DDR3`, `DDR4`, `DDR5` |
| `speed_mhz` | integer | no | 800 - 8400 (e.g. 2133, 2400, 2666, 3200) |
| `ecc` | boolean | no | |
| `registered` | boolean | no | |
| `form_factor` | string | no | |
| `rank` | string | no | |
| `voltage` | string | no | |
| `compatible_servers` | [string] | no | |

### Drive

| Field | Type | Required | Values |
|-------|------|----------|--------|
| `manufacturer` | string | no | |
| `part_number` | string | no | |
| `capacity` | string | yes | |
| `capacity_bytes` | integer | no | |
| `interface` | enum | yes | `SAS`, `SATA`, `NVMe`, `U.2` |
| `form_factor` | enum | no | `2.5`, `3.5` |
| `type` | enum | no | `SSD`, `HDD` |
| `rpm` | integer | no | |
| `endurance` | string | no | |
| `encryption` | boolean | no | |
| `carrier_included` | boolean | no | |
| `carrier_type` | string | no | |

### Server

| Field | Type | Required | Values |
|-------|------|----------|--------|
| `manufacturer` | string | yes | |
| `model` | string | yes | |
| `generation` | string | no | |
| `form_factor` | enum | no | `1U`, `2U`, `4U`, `tower` |
| `drive_bays` | string | no | |
| `drive_form_factor` | string | no | |
| `cpu_count` | integer | no | |
| `cpu_model` | string | no | |
| `cpu_installed` | boolean | no | |
| `ram_total_gb` | integer | no | |
| `ram_stick_count` | integer | no | |
| `ram_slots_total` | integer | no | |
| `drives_included` | boolean | no | |
| `drive_blanks_included` | boolean | no | |
| `raid_controller` | string | no | |
| `power_supplies` | integer | no | |
| `idrac_license` | string | no | |
| `ilo_license` | string | no | |
| `rails_included` | boolean | no | |
| `bezel_included` | boolean | no | |
| `network_card` | string | no | |
| `boots_tested` | boolean | no | |

### CPU

| Field | Type | Required | Values |
|-------|------|----------|--------|
| `manufacturer` | enum | yes | `Intel`, `AMD` |
| `family` | enum | yes | `Xeon`, `EPYC` |
| `series` | string | no | |
| `model` | string | yes | |
| `generation` | string | no | |
| `cores` | integer | no | 1 - 256 |
| `threads` | integer | no | |
| `base_clock_ghz` | float | no | 0.5 - 6.0 |
| `boost_clock_ghz` | float | no | |
| `tdp_watts` | integer | no | 10 - 500 |
| `socket` | string | no | |
| `l3_cache_mb` | integer | no | |
| `part_number` | string | no | |
| `matched_pair` | boolean | no | |

### NIC

| Field | Type | Required | Values |
|-------|------|----------|--------|
| `manufacturer` | string | no | |
| `model` | string | no | |
| `speed` | enum | yes | `1GbE`, `10GbE`, `25GbE`, `40GbE`, `100GbE` |
| `port_count` | integer | yes | 1 - 8 |
| `port_type` | enum | no | `SFP+`, `SFP28`, `QSFP+`, `QSFP28`, `RJ45`, `BaseT` |
| `interface` | string | no | |
| `pcie_generation` | string | no | |
| `firmware_protocol` | string | no | |
| `part_number` | string | no | |
| `oem_part_number` | string | no | |
| `low_profile` | boolean | no | |
| `transceivers_included` | boolean | no | |
