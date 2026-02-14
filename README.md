# server price tracker

An API-first Go service that monitors eBay listings for server hardware,
extracts structured attributes via LLM (Ollama, Anthropic Claude, or compatible
backends), scores listings against historical baselines, and alerts on deals via
Discord webhooks. The CLI acts as a client to the API, and the API design
supports future integrations (Discord bot, web UI, Grafana dashboards).

## Extraction Test Results

Validated against `mistral:7b-instruct-v0.3-q5_K_M` via Ollama:

| Type | Title | Key Extractions | Product Key |
|------|-------|-----------------|-------------|
| RAM | Samsung 32GB DDR4-2666 PC4-21300 ECC Registered RDIMM | `generation: DDR4`, `speed_mhz: 2666`, `ecc: true`, `registered: true` | `ram:ddr4:ecc_reg:32gb:2666` |
| Drive | Samsung PM893 960GB SATA 2.5" SSD MZ-7L3960A Enterprise | `interface: SATA`, `form_factor: 2.5`, `type: SSD` | `drive:sata:2.5:960gb:ssd` |
| Server | Dell PowerEdge R740xd 2U 24x 2.5" SFF 2x Xeon Gold 6130 64GB DDR4 H730P | `manufacturer: Dell`, `model: PowerEdge R740xd`, `form_factor: 2U`, `cpu_count: 2` | `server:dell:poweredge_r740xd:unknown` |
| CPU | Intel Xeon Gold 6248R 3.0GHz 24-Core 35.75MB LGA3647 CPU Processor SRGZG | `family: Xeon`, `series: Gold`, `model: 6248R`, `cores: 24` | `cpu:intel:xeon:6248r` |
| NIC | Mellanox ConnectX-4 Lx CX4121A 25GbE SFP28 Dual Port PCIe 3.0 x8 Network Card | `speed: 25GbE`, `port_count: 2`, `port_type: SFP28` | `nic:25gbe:2p:sfp28` |

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
