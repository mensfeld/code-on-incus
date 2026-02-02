package limits

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// cpuCountRegex matches valid CPU count formats: "2", "0-3", "0,1,3"
	cpuCountRegex = regexp.MustCompile(`^(\d+(-\d+)?)(,\d+(-\d+)?)*$`)

	// cpuAllowancePercentRegex matches percentage format: "50%"
	cpuAllowancePercentRegex = regexp.MustCompile(`^\d+%$`)

	// cpuAllowanceTimeRegex matches time slice format: "25ms/100ms"
	cpuAllowanceTimeRegex = regexp.MustCompile(`^\d+ms/\d+ms$`)

	// memoryRegex matches memory sizes: "512MiB", "2GiB", "50%"
	memoryRegex = regexp.MustCompile(`^\d+(%|[KMGT]i?B)$`)

	// diskIORegex matches disk I/O rates: "10MiB/s", "1000iops"
	diskIORegex = regexp.MustCompile(`^(\d+[KMGT]i?B/s|\d+iops)$`)
)

// ValidateCPUCount validates CPU count format
// Valid formats: "2", "0-3", "0,1,3", "" (empty = unlimited)
func ValidateCPUCount(count string) error {
	if count == "" {
		return nil // Empty = unlimited
	}
	if !cpuCountRegex.MatchString(count) {
		return fmt.Errorf("invalid CPU count format: %s (examples: '2', '0-3', '0,1,3')", count)
	}
	return nil
}

// ValidateCPUAllowance validates CPU allowance format
// Valid formats: "50%", "25ms/100ms", "" (empty = unlimited)
func ValidateCPUAllowance(allowance string) error {
	if allowance == "" {
		return nil // Empty = unlimited
	}
	if !cpuAllowancePercentRegex.MatchString(allowance) && !cpuAllowanceTimeRegex.MatchString(allowance) {
		return fmt.Errorf("invalid CPU allowance format: %s (examples: '50%%', '25ms/100ms')", allowance)
	}
	return nil
}

// ValidatePriority validates priority value (0-10)
func ValidatePriority(priority int) error {
	if priority < 0 || priority > 10 {
		return fmt.Errorf("invalid priority: %d (must be between 0 and 10)", priority)
	}
	return nil
}

// ValidateMemoryLimit validates memory limit format
// Valid formats: "512MiB", "2GiB", "50%", "" (empty = unlimited)
func ValidateMemoryLimit(limit string) error {
	if limit == "" {
		return nil // Empty = unlimited
	}
	if !memoryRegex.MatchString(limit) {
		return fmt.Errorf("invalid memory limit: %s (examples: '512MiB', '2GiB', '50%%')", limit)
	}
	return nil
}

// ValidateMemoryEnforce validates memory enforcement mode
// Valid values: "hard", "soft", ""
func ValidateMemoryEnforce(enforce string) error {
	if enforce == "" {
		return nil // Empty = use default
	}
	if enforce != "hard" && enforce != "soft" {
		return fmt.Errorf("invalid memory enforce mode: %s (must be 'hard' or 'soft')", enforce)
	}
	return nil
}

// ValidateMemorySwap validates memory swap configuration
// Valid values: "true", "false", size like "1GiB", ""
func ValidateMemorySwap(swap string) error {
	if swap == "" {
		return nil // Empty = use default
	}
	if swap == "true" || swap == "false" {
		return nil
	}
	// Must be a valid memory size
	if !memoryRegex.MatchString(swap) {
		return fmt.Errorf("invalid memory swap: %s (must be 'true', 'false', or a size like '1GiB')", swap)
	}
	return nil
}

// ValidateDiskIO validates disk I/O rate format
// Valid formats: "10MiB/s", "1000iops", "" (empty = unlimited)
func ValidateDiskIO(io string) error {
	if io == "" {
		return nil // Empty = unlimited
	}
	if !diskIORegex.MatchString(io) {
		return fmt.Errorf("invalid disk I/O rate: %s (examples: '10MiB/s', '1000iops')", io)
	}
	return nil
}

// ValidateDuration validates duration format
// Valid formats: "2h", "30m", "1h30m", "" (empty = unlimited)
// Uses Go's time.ParseDuration
func ValidateDuration(duration string) error {
	if duration == "" {
		return nil // Empty = unlimited
	}
	_, err := time.ParseDuration(duration)
	if err != nil {
		return fmt.Errorf("invalid duration: %s (examples: '2h', '30m', '1h30m')", duration)
	}
	return nil
}

// ValidateMaxProcesses validates max processes limit
// Valid values: >= 0 (0 = unlimited)
func ValidateMaxProcesses(maxProc int) error {
	if maxProc < 0 {
		return fmt.Errorf("invalid max_processes: %d (must be >= 0, where 0 = unlimited)", maxProc)
	}
	return nil
}

// ParseDuration parses a duration string and returns the duration
// Returns 0 if the string is empty (unlimited)
func ParseDuration(duration string) (time.Duration, error) {
	if duration == "" {
		return 0, nil
	}
	return time.ParseDuration(duration)
}

// NormalizeBoolString converts "true"/"false" strings to proper boolean strings
// for Incus config (Incus expects "true"/"false" lowercase)
func NormalizeBoolString(value string) string {
	lower := strings.ToLower(value)
	if lower == "true" || lower == "false" {
		return lower
	}
	return value
}

// ValidateAll validates all limit configurations in one pass
// Returns a map of field names to error messages
func ValidateAll(cpu CPULimits, memory MemoryLimits, disk DiskLimits, runtime RuntimeLimits) map[string]error {
	errors := make(map[string]error)

	// Validate CPU limits
	if err := ValidateCPUCount(cpu.Count); err != nil {
		errors["cpu.count"] = err
	}
	if err := ValidateCPUAllowance(cpu.Allowance); err != nil {
		errors["cpu.allowance"] = err
	}
	if err := ValidatePriority(cpu.Priority); err != nil {
		errors["cpu.priority"] = err
	}

	// Validate memory limits
	if err := ValidateMemoryLimit(memory.Limit); err != nil {
		errors["memory.limit"] = err
	}
	if err := ValidateMemoryEnforce(memory.Enforce); err != nil {
		errors["memory.enforce"] = err
	}
	if err := ValidateMemorySwap(memory.Swap); err != nil {
		errors["memory.swap"] = err
	}

	// Validate disk limits
	if err := ValidateDiskIO(disk.Read); err != nil {
		errors["disk.read"] = err
	}
	if err := ValidateDiskIO(disk.Write); err != nil {
		errors["disk.write"] = err
	}
	if err := ValidateDiskIO(disk.Max); err != nil {
		errors["disk.max"] = err
	}
	if err := ValidatePriority(disk.Priority); err != nil {
		errors["disk.priority"] = err
	}

	// Validate runtime limits
	if err := ValidateDuration(runtime.MaxDuration); err != nil {
		errors["runtime.max_duration"] = err
	}
	if err := ValidateMaxProcesses(runtime.MaxProcesses); err != nil {
		errors["runtime.max_processes"] = err
	}

	if len(errors) == 0 {
		return nil
	}
	return errors
}

// CPULimits represents CPU resource limits
type CPULimits struct {
	Count     string
	Allowance string
	Priority  int
}

// MemoryLimits represents memory resource limits
type MemoryLimits struct {
	Limit   string
	Enforce string
	Swap    string
}

// DiskLimits represents disk I/O resource limits
type DiskLimits struct {
	Read     string
	Write    string
	Max      string
	Priority int
}

// RuntimeLimits represents time-based and process limits
type RuntimeLimits struct {
	MaxDuration  string
	MaxProcesses int
	AutoStop     bool
	StopGraceful bool
}

// FormatValidationErrors formats a map of validation errors into a readable string
func FormatValidationErrors(errors map[string]error) string {
	if len(errors) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("validation errors:\n")
	for field, err := range errors {
		sb.WriteString(fmt.Sprintf("  - %s: %v\n", field, err))
	}
	return sb.String()
}

// ValidateCPUCountValue validates and parses CPU count value
// Returns error if the count value is invalid (e.g., range end < start)
func ValidateCPUCountValue(count string) error {
	if count == "" {
		return nil
	}

	// Check basic format
	if err := ValidateCPUCount(count); err != nil {
		return err
	}

	// Parse ranges and check validity
	parts := strings.Split(count, ",")
	for _, part := range parts {
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return fmt.Errorf("invalid CPU range: %s", part)
			}
			start, err1 := strconv.Atoi(rangeParts[0])
			end, err2 := strconv.Atoi(rangeParts[1])
			if err1 != nil || err2 != nil {
				return fmt.Errorf("invalid CPU range values: %s", part)
			}
			if end < start {
				return fmt.Errorf("invalid CPU range (end < start): %s", part)
			}
		} else {
			if _, err := strconv.Atoi(part); err != nil {
				return fmt.Errorf("invalid CPU number: %s", part)
			}
		}
	}

	return nil
}
