package extract

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// classifyTmpl is the classification prompt template.
const classifyTmpl = `Classify this eBay listing into exactly one component type.

Title: {{.Title}}

Types: ram, drive, server, cpu, nic, other

Respond with ONLY a single word from the list above. No explanation, no parentheses, no extra text.`

// ramTmpl is the RAM extraction prompt template.
const ramTmpl = `Extract structured attributes from this eBay server RAM listing.
Respond ONLY with a valid JSON object. No markdown, no explanation.

Rules:
- For enum fields, you MUST use one of the listed values exactly. Never return null for enum fields.
- "condition": if the listing does not specify, use "unknown".
- "confidence": always return a float between 0.0 and 1.0. Never null.
- "quantity": default to 1 unless the title explicitly indicates a lot or bundle.
- "speed_mhz": Convert PC module numbers to MHz speed. Common mappings:
  PC3-10600=1333, PC3-12800=1600, PC3-14900=1866,
  PC4-17000=2133, PC4-19200=2400, PC4-21300=2666, PC4-23400=2933, PC4-25600=3200,
  PC5-38400=4800, PC5-44800=5600, PC5-51200=6400.
  Ignore any letter suffix (V, R, T, U, E) after the number. Always convert when present.
- Only use null for optional string/integer/boolean fields that truly cannot be determined.

Title: {{.Title}}
Item Specifics: {{.ItemSpecifics}}

Schema:
{
  "manufacturer": string | null,
  "part_number": string | null,
  "capacity_gb": integer | null,
  "quantity": integer,
  "generation": "DDR3" | "DDR4" | "DDR5",
  "speed_mhz": integer (e.g. 2133, 2400, 2666, 3200; derived from PC module number if present) | null,
  "ecc": boolean | null,
  "registered": boolean | null,
  "form_factor": string | null,
  "rank": string | null,
  "voltage": string | null,
  "condition": "new" | "like_new" | "used_working" | "for_parts" | "unknown",
  "compatible_servers": [string],
  "confidence": float (0.0-1.0)
}`

// driveTmpl is the drive extraction prompt template.
const driveTmpl = `Extract structured attributes from this eBay server drive listing.
Respond ONLY with a valid JSON object. No markdown, no explanation.

Rules:
- For enum fields, you MUST use one of the listed values exactly. Never return null for enum fields.
- "interface": determine from the title (e.g. "SATA", "SAS", "NVMe", "U.2"). Never null.
- "form_factor": use only "2.5" or "3.5" as plain strings without quotes or inch marks.
- "condition": if the listing does not specify, use "unknown".
- "confidence": always return a float between 0.0 and 1.0. Never null.
- "quantity": default to 1 unless the title explicitly indicates a lot or bundle.
- Only use null for optional string/integer/boolean fields that truly cannot be determined.

Title: {{.Title}}
Item Specifics: {{.ItemSpecifics}}

Schema:
{
  "manufacturer": string | null,
  "part_number": string | null,
  "capacity": string | null,
  "capacity_bytes": integer | null,
  "quantity": integer,
  "interface": "SAS" | "SATA" | "NVMe" | "U.2",
  "form_factor": "2.5" | "3.5" | null,
  "type": "SSD" | "HDD",
  "rpm": integer | null,
  "endurance": string | null,
  "encryption": boolean | null,
  "carrier_included": boolean | null,
  "carrier_type": string | null,
  "condition": "new" | "like_new" | "used_working" | "for_parts" | "unknown",
  "confidence": float (0.0-1.0)
}`

// serverTmpl is the server extraction prompt template.
const serverTmpl = `Extract structured attributes from this eBay server listing.
Respond ONLY with a valid JSON object. No markdown, no explanation.

Rules:
- For enum fields, you MUST use one of the listed values exactly. Never return null for enum fields.
- "manufacturer" and "model" are required. Never null.
- "condition": if the listing does not specify, use "unknown".
- "confidence": always return a float between 0.0 and 1.0. Never null.
- "quantity": default to 1 unless the title explicitly indicates a lot or bundle.
- Only use null for optional string/integer/boolean fields that truly cannot be determined.

Title: {{.Title}}
Item Specifics: {{.ItemSpecifics}}
Description (first 500 chars): {{.Description}}

Schema:
{
  "manufacturer": string,
  "model": string,
  "generation": string | null,
  "form_factor": "1U" | "2U" | "4U" | "tower" | null,
  "drive_bays": string | null,
  "drive_form_factor": string | null,
  "cpu_count": integer | null,
  "cpu_model": string | null,
  "cpu_installed": boolean | null,
  "ram_total_gb": integer | null,
  "ram_stick_count": integer | null,
  "ram_slots_total": integer | null,
  "drives_included": boolean | null,
  "drive_blanks_included": boolean | null,
  "raid_controller": string | null,
  "power_supplies": integer | null,
  "idrac_license": string | null,
  "ilo_license": string | null,
  "rails_included": boolean | null,
  "bezel_included": boolean | null,
  "network_card": string | null,
  "quantity": integer,
  "condition": "new" | "like_new" | "used_working" | "for_parts" | "unknown",
  "boots_tested": boolean | null,
  "confidence": float (0.0-1.0)
}`

// cpuTmpl is the CPU extraction prompt template.
const cpuTmpl = `Extract structured attributes from this eBay server CPU listing.
Respond ONLY with a valid JSON object. No markdown, no explanation.

Rules:
- For enum fields, you MUST use one of the listed values exactly. Never return null for enum fields.
- "manufacturer": must be "Intel" or "AMD".
- "family": must be "Xeon" or "EPYC". For "Xeon Gold 6248R", family is "Xeon", series is "Gold", model is "6248R".
- "condition": if the listing does not specify, use "unknown".
- "confidence": always return a float between 0.0 and 1.0. Never null.
- "quantity": default to 1 unless the title explicitly indicates a lot or bundle.
- Only use null for optional string/integer/boolean fields that truly cannot be determined.

Title: {{.Title}}
Item Specifics: {{.ItemSpecifics}}

Schema:
{
  "manufacturer": "Intel" | "AMD",
  "family": "Xeon" | "EPYC",
  "series": string | null,
  "model": string,
  "generation": string | null,
  "cores": integer | null,
  "threads": integer | null,
  "base_clock_ghz": float | null,
  "boost_clock_ghz": float | null,
  "tdp_watts": integer | null,
  "socket": string | null,
  "l3_cache_mb": integer | null,
  "part_number": string | null,
  "quantity": integer,
  "condition": "new" | "like_new" | "used_working" | "for_parts" | "unknown",
  "matched_pair": boolean | null,
  "confidence": float (0.0-1.0)
}`

// nicTmpl is the NIC extraction prompt template.
const nicTmpl = `Extract structured attributes from this eBay server network card listing.
Respond ONLY with a valid JSON object. No markdown, no explanation.

Rules:
- For enum fields, you MUST use one of the listed values exactly. Never return null for enum fields.
- "speed": must be one of the listed values. Determine from the title (e.g. "25GbE", "10GbE").
- "port_count": determine from the title (e.g. "Dual Port" = 2, "Quad Port" = 4). Never null.
- "condition": if the listing does not specify, use "unknown".
- "confidence": always return a float between 0.0 and 1.0. Never null.
- "quantity": default to 1 unless the title explicitly indicates a lot or bundle.
- Only use null for optional string/integer/boolean fields that truly cannot be determined.

Title: {{.Title}}
Item Specifics: {{.ItemSpecifics}}

Schema:
{
  "manufacturer": string | null,
  "model": string | null,
  "speed": "1GbE" | "10GbE" | "25GbE" | "40GbE" | "100GbE",
  "port_count": integer (1-8),
  "port_type": "SFP+" | "SFP28" | "QSFP+" | "QSFP28" | "RJ45" | "BaseT" | null,
  "interface": string | null,
  "pcie_generation": string | null,
  "firmware_protocol": string | null,
  "part_number": string | null,
  "oem_part_number": string | null,
  "low_profile": boolean | null,
  "transceivers_included": boolean | null,
  "quantity": integer,
  "condition": "new" | "like_new" | "used_working" | "for_parts" | "unknown",
  "confidence": float (0.0-1.0)
}`

// PromptData holds the template variables for extraction prompts.
type PromptData struct {
	Title         string
	ItemSpecifics string
	Description   string // only used by server prompts
}

var templates map[domain.ComponentType]*template.Template

func init() {
	templates = map[domain.ComponentType]*template.Template{
		domain.ComponentRAM:    template.Must(template.New("ram").Parse(ramTmpl)),
		domain.ComponentDrive:  template.Must(template.New("drive").Parse(driveTmpl)),
		domain.ComponentServer: template.Must(template.New("server").Parse(serverTmpl)),
		domain.ComponentCPU:    template.Must(template.New("cpu").Parse(cpuTmpl)),
		domain.ComponentNIC:    template.Must(template.New("nic").Parse(nicTmpl)),
	}
}

var classifyTemplate = template.Must(template.New("classify").Parse(classifyTmpl))

// RenderClassifyPrompt renders the classification prompt for a title.
func RenderClassifyPrompt(title string) (string, error) {
	var buf bytes.Buffer
	if err := classifyTemplate.Execute(&buf, PromptData{Title: title}); err != nil {
		return "", fmt.Errorf("rendering classify prompt: %w", err)
	}
	return buf.String(), nil
}

// RenderExtractPrompt renders the extraction prompt for a given component type.
func RenderExtractPrompt(
	componentType domain.ComponentType,
	title string,
	itemSpecifics map[string]string,
) (string, error) {
	tmpl, ok := templates[componentType]
	if !ok {
		return "", fmt.Errorf("no extraction prompt for component type %q", componentType)
	}

	var buf bytes.Buffer
	data := PromptData{
		Title:         title,
		ItemSpecifics: formatItemSpecifics(itemSpecifics),
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering %s extraction prompt: %w", componentType, err)
	}

	return buf.String(), nil
}

// RenderServerExtractPrompt renders the server extraction prompt with description.
func RenderServerExtractPrompt(
	title string,
	itemSpecifics map[string]string,
	description string,
) (string, error) {
	var buf bytes.Buffer
	data := PromptData{
		Title:         title,
		ItemSpecifics: formatItemSpecifics(itemSpecifics),
		Description:   description,
	}

	tmpl := templates[domain.ComponentServer]
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering server extraction prompt: %w", err)
	}

	return buf.String(), nil
}

func formatItemSpecifics(specs map[string]string) string {
	if len(specs) == 0 {
		return "N/A"
	}

	var parts []string
	for k, v := range specs {
		parts = append(parts, k+": "+v)
	}

	return strings.Join(parts, ", ")
}
