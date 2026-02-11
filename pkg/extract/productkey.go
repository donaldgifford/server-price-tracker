package extract

import (
	"fmt"
	"strings"
)

// ProductKey generates a normalized product key for baseline grouping.
// The key format varies by component type per EXTRACTION.md rules.
func ProductKey(componentType string, attrs map[string]any) string {
	switch componentType {
	case "ram":
		return fmt.Sprintf("ram:%s:%s:%dgb:%d",
			normalizeStr(attrs["generation"]),
			ramType(attrs),
			pkInt(attrs, "capacity_gb"),
			pkInt(attrs, "speed_mhz"),
		)
	case "drive":
		return fmt.Sprintf("drive:%s:%s:%s:%s",
			normalizeStr(attrs["interface"]),
			normalizeStr(attrs["form_factor"]),
			normalizeStr(attrs["capacity"]),
			driveType(attrs),
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
			pkInt(attrs, "port_count"),
			normalizeStr(attrs["port_type"]),
		)
	default:
		return fmt.Sprintf("other:%s", componentType)
	}
}

const unknownKey = "unknown"

func normalizeStr(v any) string {
	if v == nil {
		return unknownKey
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return unknownKey
	}
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func pkInt(attrs map[string]any, key string) int {
	v, ok := attrs[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

// ramType determines the RAM sub-type from ECC and registered flags.
func ramType(attrs map[string]any) string {
	ecc, hasECC := attrs["ecc"]
	if !hasECC || ecc == nil {
		return unknownKey
	}

	eccBool, ok := ecc.(bool)
	if !ok {
		return unknownKey
	}

	if eccBool {
		reg, hasReg := attrs["registered"]
		if hasReg && reg != nil {
			if regBool, ok := reg.(bool); ok && regBool {
				return "ecc_reg"
			}
		}
		return "ecc_unbuf"
	}
	return "non_ecc"
}

// driveType determines the drive type for product key grouping.
func driveType(attrs map[string]any) string {
	dt, ok := attrs["type"]
	if !ok || dt == nil {
		return unknownKey
	}

	s, ok := dt.(string)
	if !ok {
		return unknownKey
	}

	if strings.EqualFold(s, "SSD") {
		return "ssd"
	}

	rpm := pkInt(attrs, "rpm")
	switch rpm {
	case 7200:
		return "7k2"
	case 10000:
		return "10k"
	case 15000:
		return "15k"
	default:
		return "hdd"
	}
}
