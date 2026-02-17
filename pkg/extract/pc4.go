package extract

import (
	"regexp"
	"strconv"
	"strings"
)

// pc4SpeedTable maps PC module bandwidth numbers to MHz speeds.
// Keys are the numeric portion only (e.g., "21300" not "PC4-21300").
var pc4SpeedTable = map[string]int{
	// DDR3
	"10600": 1333,
	"12800": 1600,
	"14900": 1866,
	// DDR4
	"17000": 2133,
	"19200": 2400,
	"21300": 2666,
	"23400": 2933,
	"25600": 3200,
	// DDR5
	"38400": 4800,
	"44800": 5600,
	"51200": 6400,
}

// pc4Regex matches PC module designations in listing titles.
// Captures the numeric bandwidth portion (group 1).
// Examples: "PC4-21300", "PC4-21300V", "pc3-12800".
var pc4Regex = regexp.MustCompile(`(?i)\bPC[345]-?(\d{5,6})[A-Z]?\b`)

// ddrSpeedRegex matches DDR speed designations as a fallback.
// Captures the 4-digit MHz speed (group 1).
// Examples: "DDR4-2666", "DDR4-2400T", "DDR5-4800".
var ddrSpeedRegex = regexp.MustCompile(`(?i)\bDDR[345]-(\d{4})\b`)

// PC4ToMHz converts a PC module designation string to its MHz speed.
// Accepts formats like "PC4-21300", "PC4-21300V", "pc4-21300", or just "21300".
// Returns (mhz, true) on match, (0, false) on miss.
func PC4ToMHz(moduleNumber string) (int, bool) {
	s := strings.ToUpper(strings.TrimSpace(moduleNumber))

	// Strip PC3-/PC4-/PC5- prefix if present.
	for _, prefix := range []string{"PC3-", "PC4-", "PC5-", "PC3", "PC4", "PC5"} {
		if after, found := strings.CutPrefix(s, prefix); found {
			s = after
			break
		}
	}

	// Strip trailing letter suffix (V, R, T, U, E, etc.).
	if s != "" {
		last := s[len(s)-1]
		if last >= 'A' && last <= 'Z' {
			s = s[:len(s)-1]
		}
	}

	if s == "" {
		return 0, false
	}

	mhz, ok := pc4SpeedTable[s]
	return mhz, ok
}

// ExtractSpeedFromTitle attempts to extract RAM speed in MHz from a listing
// title. It first tries PC module number patterns (e.g., "PC4-21300" → 2666),
// then falls back to DDR speed patterns (e.g., "DDR4-2666" → 2666).
// Returns (mhz, true) on match, (0, false) if no speed pattern is found.
func ExtractSpeedFromTitle(title string) (int, bool) {
	// Try PC module number first (most reliable conversion).
	if match := pc4Regex.FindStringSubmatch(title); len(match) > 1 {
		if mhz, ok := pc4SpeedTable[match[1]]; ok {
			return mhz, true
		}
	}

	// Fall back to DDR speed designation.
	if match := ddrSpeedRegex.FindStringSubmatch(title); len(match) > 1 {
		speed, err := strconv.Atoi(match[1])
		if err != nil {
			return 0, false
		}
		// Validate against reasonable server RAM speed range.
		if speed >= 800 && speed <= 8400 {
			return speed, true
		}
	}

	return 0, false
}

// NormalizeRAMSpeed fills in speed_mhz from PC module numbers in the title
// when the LLM returned null or 0. Modifies attrs in place.
// Returns true if speed_mhz is set (was already present or was recovered).
func NormalizeRAMSpeed(title string, attrs map[string]any) bool {
	// Check if speed_mhz is already set and non-zero.
	if v, ok := attrs["speed_mhz"]; ok && v != nil {
		switch n := v.(type) {
		case int:
			if n != 0 {
				return true
			}
		case float64:
			if int(n) != 0 {
				return true
			}
		}
	}

	// Attempt to extract speed from the title.
	if mhz, ok := ExtractSpeedFromTitle(title); ok {
		attrs["speed_mhz"] = mhz
		return true
	}

	return false
}
