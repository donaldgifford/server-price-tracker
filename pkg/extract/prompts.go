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

Respond with only the type.`

// ramTmpl is the RAM extraction prompt template.
const ramTmpl = `Extract structured attributes from this eBay server RAM listing.
Respond ONLY with a JSON object matching the schema below.
If a field cannot be determined, use null.
For quantity, default to 1 unless the title/description explicitly indicates a lot or bundle.

Title: {{.Title}}
Item Specifics: {{.ItemSpecifics}}

Schema:
{
  "manufacturer": string | null,
  "part_number": string | null,
  "capacity_gb": integer | null,
  "quantity": integer,
  "generation": "DDR3" | "DDR4" | "DDR5",
  "speed_mhz": integer (e.g. 2133, 2400, 2666, 3200) | null,
  "ecc": boolean | null,
  "registered": boolean | null,
  "form_factor": string | null,
  "rank": string | null,
  "voltage": string | null,
  "condition": "new" | "like_new" | "used_working" | "for_parts" | "unknown",
  "compatible_servers": [string],
  "confidence": float
}`

// driveTmpl is the drive extraction prompt template.
const driveTmpl = `Extract structured attributes from this eBay server drive listing.
Respond ONLY with a JSON object. If a field cannot be determined, use null.

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
  "confidence": float
}`

// serverTmpl is the server extraction prompt template.
const serverTmpl = `Extract structured attributes from this eBay server listing.
Respond ONLY with a JSON object. If a field cannot be determined, use null.

Title: {{.Title}}
Item Specifics: {{.ItemSpecifics}}
Description (first 500 chars): {{.Description}}

Schema:
{
  "manufacturer": string | null,
  "model": string | null,
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
  "confidence": float
}`

// cpuTmpl is the CPU extraction prompt template.
const cpuTmpl = `Extract structured attributes from this eBay server CPU listing.
Respond ONLY with a JSON object. If a field cannot be determined, use null.

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
  "confidence": float
}`

// nicTmpl is the NIC extraction prompt template.
const nicTmpl = `Extract structured attributes from this eBay server network card listing.
Respond ONLY with a JSON object. If a field cannot be determined, use null.

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
  "confidence": float
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
