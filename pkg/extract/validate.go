package extract

import (
	"errors"
	"fmt"
	"slices"

	domain "github.com/donaldgifford/server-price-tracker/pkg/types"
)

// Validation errors.
var (
	ErrMissingField = errors.New("missing required field")
	ErrOutOfRange   = errors.New("value out of valid range")
	ErrInvalidEnum  = errors.New("invalid enum value")
)

// ValidateExtraction validates extracted attributes for a given component type.
func ValidateExtraction(
	componentType domain.ComponentType,
	attrs map[string]any,
) error {
	if err := validateCommon(attrs); err != nil {
		return err
	}

	switch componentType {
	case domain.ComponentRAM:
		return validateRAM(attrs)
	case domain.ComponentDrive:
		return validateDrive(attrs)
	case domain.ComponentServer:
		return validateServer(attrs)
	case domain.ComponentCPU:
		return validateCPU(attrs)
	case domain.ComponentNIC:
		return validateNIC(attrs)
	default:
		return nil
	}
}

var validConditions = []string{
	"new", "like_new", "used_working", "for_parts", "unknown",
}

func validateCommon(attrs map[string]any) error {
	// condition: required enum (normalize LLM output before validating)
	cond, ok := attrString(attrs, "condition")
	if !ok {
		return fmt.Errorf("condition: %w", ErrMissingField)
	}
	normalized := string(NormalizeCondition(cond))
	if !slices.Contains(validConditions, normalized) {
		return fmt.Errorf("condition %q: %w", cond, ErrInvalidEnum)
	}
	attrs["condition"] = normalized

	// confidence: 0.0-1.0
	conf, ok := attrFloat(attrs, "confidence")
	if !ok {
		return fmt.Errorf("confidence: %w", ErrMissingField)
	}
	if conf < 0.0 || conf > 1.0 {
		return fmt.Errorf("confidence %.2f: %w (must be 0.0-1.0)", conf, ErrOutOfRange)
	}

	// quantity: >= 1
	qty, ok := attrInt(attrs, "quantity")
	if ok && qty < 1 {
		return fmt.Errorf("quantity %d: %w (must be >= 1)", qty, ErrOutOfRange)
	}

	return nil
}

var validRAMGenerations = []string{"DDR3", "DDR4", "DDR5"}

func validateRAM(attrs map[string]any) error {
	// capacity_gb: 1-1024 (required)
	capacity, ok := attrInt(attrs, "capacity_gb")
	if !ok {
		return fmt.Errorf("capacity_gb: %w", ErrMissingField)
	}
	if capacity < 1 || capacity > 1024 {
		return fmt.Errorf("capacity_gb %d: %w (must be 1-1024)", capacity, ErrOutOfRange)
	}

	// generation: required enum
	gen, ok := attrString(attrs, "generation")
	if !ok {
		return fmt.Errorf("generation: %w", ErrMissingField)
	}
	if !slices.Contains(validRAMGenerations, gen) {
		return fmt.Errorf("generation %q: %w", gen, ErrInvalidEnum)
	}

	// speed_mhz: 800-8400 (optional)
	if spd, ok := attrInt(attrs, "speed_mhz"); ok {
		if spd < 800 || spd > 8400 {
			return fmt.Errorf("speed_mhz %d: %w (must be 800-8400)", spd, ErrOutOfRange)
		}
	}

	return nil
}

var (
	validDriveInterfaces = []string{"SAS", "SATA", "NVMe", "U.2"}
	validDriveFormFactor = []string{"2.5", "3.5"}
	validDriveTypes      = []string{"SSD", "HDD"}
)

func validateDrive(attrs map[string]any) error {
	// capacity: non-empty (required)
	capacity, ok := attrString(attrs, "capacity")
	if !ok || capacity == "" {
		return fmt.Errorf("capacity: %w", ErrMissingField)
	}

	// interface: required enum
	iface, ok := attrString(attrs, "interface")
	if !ok {
		return fmt.Errorf("interface: %w", ErrMissingField)
	}
	if !slices.Contains(validDriveInterfaces, iface) {
		return fmt.Errorf("interface %q: %w", iface, ErrInvalidEnum)
	}

	// form_factor: optional enum
	if ff, ok := attrString(attrs, "form_factor"); ok {
		if !slices.Contains(validDriveFormFactor, ff) {
			return fmt.Errorf("form_factor %q: %w", ff, ErrInvalidEnum)
		}
	}

	// type: optional enum
	if dt, ok := attrString(attrs, "type"); ok {
		if !slices.Contains(validDriveTypes, dt) {
			return fmt.Errorf("type %q: %w", dt, ErrInvalidEnum)
		}
	}

	return nil
}

var validServerFormFactors = []string{"1U", "2U", "4U", "tower"}

func validateServer(attrs map[string]any) error {
	// manufacturer: non-empty (required)
	mfr, ok := attrString(attrs, "manufacturer")
	if !ok || mfr == "" {
		return fmt.Errorf("manufacturer: %w", ErrMissingField)
	}

	// model: non-empty (required)
	model, ok := attrString(attrs, "model")
	if !ok || model == "" {
		return fmt.Errorf("model: %w", ErrMissingField)
	}

	// form_factor: optional enum
	if ff, ok := attrString(attrs, "form_factor"); ok {
		if !slices.Contains(validServerFormFactors, ff) {
			return fmt.Errorf("form_factor %q: %w", ff, ErrInvalidEnum)
		}
	}

	return nil
}

var (
	validCPUManufacturers = []string{"Intel", "AMD"}
	validCPUFamilies      = []string{"Xeon", "EPYC"}
)

func validateCPU(attrs map[string]any) error {
	if err := validateCPURequired(attrs); err != nil {
		return err
	}
	return validateCPUOptional(attrs)
}

func validateCPURequired(attrs map[string]any) error {
	// manufacturer: required enum
	mfr, ok := attrString(attrs, "manufacturer")
	if !ok {
		return fmt.Errorf("manufacturer: %w", ErrMissingField)
	}
	if !slices.Contains(validCPUManufacturers, mfr) {
		return fmt.Errorf("manufacturer %q: %w", mfr, ErrInvalidEnum)
	}

	// family: required enum
	fam, ok := attrString(attrs, "family")
	if !ok {
		return fmt.Errorf("family: %w", ErrMissingField)
	}
	if !slices.Contains(validCPUFamilies, fam) {
		return fmt.Errorf("family %q: %w", fam, ErrInvalidEnum)
	}

	// model: non-empty (required)
	model, ok := attrString(attrs, "model")
	if !ok || model == "" {
		return fmt.Errorf("model: %w", ErrMissingField)
	}

	return nil
}

func validateCPUOptional(attrs map[string]any) error {
	// cores: 1-256 (optional)
	if cores, ok := attrInt(attrs, "cores"); ok {
		if cores < 1 || cores > 256 {
			return fmt.Errorf("cores %d: %w (must be 1-256)", cores, ErrOutOfRange)
		}
	}

	// base_clock_ghz: 0.5-6.0 (optional)
	if clk, ok := attrFloat(attrs, "base_clock_ghz"); ok {
		if clk < 0.5 || clk > 6.0 {
			return fmt.Errorf(
				"base_clock_ghz %.2f: %w (must be 0.5-6.0)",
				clk,
				ErrOutOfRange,
			)
		}
	}

	// tdp_watts: 10-500 (optional)
	if tdp, ok := attrInt(attrs, "tdp_watts"); ok {
		if tdp < 10 || tdp > 500 {
			return fmt.Errorf("tdp_watts %d: %w (must be 10-500)", tdp, ErrOutOfRange)
		}
	}

	return nil
}

var (
	validNICSpeeds    = []string{"1GbE", "10GbE", "25GbE", "40GbE", "100GbE"}
	validNICPortTypes = []string{"SFP+", "SFP28", "QSFP+", "QSFP28", "RJ45", "BaseT"}
)

func validateNIC(attrs map[string]any) error {
	// speed: required enum
	spd, ok := attrString(attrs, "speed")
	if !ok {
		return fmt.Errorf("speed: %w", ErrMissingField)
	}
	if !slices.Contains(validNICSpeeds, spd) {
		return fmt.Errorf("speed %q: %w", spd, ErrInvalidEnum)
	}

	// port_count: 1-8 (required)
	pc, ok := attrInt(attrs, "port_count")
	if !ok {
		return fmt.Errorf("port_count: %w", ErrMissingField)
	}
	if pc < 1 || pc > 8 {
		return fmt.Errorf("port_count %d: %w (must be 1-8)", pc, ErrOutOfRange)
	}

	// port_type: optional enum
	if pt, ok := attrString(attrs, "port_type"); ok {
		if !slices.Contains(validNICPortTypes, pt) {
			return fmt.Errorf("port_type %q: %w", pt, ErrInvalidEnum)
		}
	}

	return nil
}

// attrString extracts a string attribute, returning false if nil or not a string.
func attrString(attrs map[string]any, key string) (string, bool) {
	v, ok := attrs[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// attrInt extracts an integer attribute, handling both int and float64 (JSON numbers).
func attrInt(attrs map[string]any, key string) (int, bool) {
	v, ok := attrs[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// attrFloat extracts a float64 attribute.
func attrFloat(attrs map[string]any, key string) (float64, bool) {
	v, ok := attrs[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}
