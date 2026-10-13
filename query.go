package remedy

import (
	"errors"
	"fmt"
	"strings"
)

// Query builds AR System qualification strings in a type-safe manner.
//
// Example usage:
//
//	q := remedy.NewQuery().
//	    And("Status", "=", "Open").
//	    And("Priority", "<", 3).
//	    Build()
//	// Result: 'Status' = "Open" AND 'Priority' < 3
type Query struct {
	conditions []string
	err        error // stores first validation error for BuildSafe
}

// validOperators contains the allowed operators for safe query building.
var validOperators = map[string]bool{
	"=":    true,
	"!=":   true,
	"<":    true,
	"<=":   true,
	">":    true,
	">=":   true,
	"LIKE": true,
}

// NewQuery creates a new empty query builder.
func NewQuery() *Query {
	return &Query{}
}

// And adds a condition with AND conjunction.
func (q *Query) And(field, op string, value any) *Query {
	q.addCondition("AND", field, op, value)
	return q
}

// Or adds a condition with OR conjunction.
func (q *Query) Or(field, op string, value any) *Query {
	q.addCondition("OR", field, op, value)
	return q
}

// AndSafe adds a condition with AND conjunction, validating the operator.
// If the operator is invalid, the error is stored and returned by BuildSafe.
func (q *Query) AndSafe(field, op string, value any) *Query {
	if err := validateOperator(op); err != nil && q.err == nil {
		q.err = err
	}
	q.addCondition("AND", field, op, value)
	return q
}

// OrSafe adds a condition with OR conjunction, validating the operator.
// If the operator is invalid, the error is stored and returned by BuildSafe.
func (q *Query) OrSafe(field, op string, value any) *Query {
	if err := validateOperator(op); err != nil && q.err == nil {
		q.err = err
	}
	q.addCondition("OR", field, op, value)
	return q
}

// Raw adds a raw qualification string with AND conjunction.
// Use this for complex expressions that can't be built with And/Or.
//
// The qualification is inserted as given, inside parentheses, with no quoting
// or escaping. The caller must quote and escape any values and field names in
// it, and must not pass untrusted input, which would allow qualification
// injection. Use And or Or for values that need escaping.
func (q *Query) Raw(qualification string) *Query {
	if len(q.conditions) > 0 {
		q.conditions = append(q.conditions, "AND")
	}
	q.conditions = append(q.conditions, "("+qualification+")")

	return q
}

// Build returns the complete qualification string.
func (q *Query) Build() string {
	return strings.Join(q.conditions, " ")
}

// BuildSafe returns the qualification string and any validation errors.
// Use this with AndSafe/OrSafe for validated query building.
func (q *Query) BuildSafe() (string, error) {
	if q.err != nil {
		return "", q.err
	}
	return strings.Join(q.conditions, " "), nil
}

// validateOperator checks if the operator is in the allowed list.
func validateOperator(op string) error {
	if !validOperators[op] {
		return errors.New("invalid operator: " + op)
	}
	return nil
}

// addCondition adds a condition with the specified conjunction.
func (q *Query) addCondition(conjunction, field, op string, value any) {
	if len(q.conditions) > 0 {
		q.conditions = append(q.conditions, conjunction)
	}

	condition := formatCondition(field, op, value)
	q.conditions = append(q.conditions, condition)
}

// formatCondition formats a single field condition.
// Field names have single quotes doubled (escapeFieldName) and string values
// have double quotes doubled (quoteValue) to prevent injection.
func formatCondition(field, op string, value any) string {
	formattedValue := formatValue(value)
	escapedField := escapeFieldName(field)
	return fmt.Sprintf("'%s' %s %s", escapedField, op, formattedValue)
}

// escapeFieldName escapes single quotes in field names by doubling them.
// This prevents query injection attacks via malicious field names.
func escapeFieldName(field string) string {
	return strings.ReplaceAll(field, "'", "''")
}

// quoteValue renders s as an AR System string literal: it wraps s in double
// quotes and doubles every double quote inside it. BMC documents this rule for
// "Nonnumeric that include quotation marks" in Remedy AR System 20.02, "Using
// fields in qualifications":
// https://docs.bmc.com/docs/ars2002/using-fields-in-qualifications-909637143.html
//
// Every other byte, including backslashes, newlines and invalid UTF-8, is
// copied unchanged. UNVERIFIED: the BMC documentation does not say how the AR
// server treats a backslash inside a string literal, and this has not been
// checked against a live server. If AR also treats a backslash before a double
// quote as an escape, a value ending in a backslash could leave the
// literal open past its closing quote, so following text would join it.
func quoteValue(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// formatValue converts a Go value to AR qualification string format. Strings,
// and the %v text of any type not listed below (for example a fmt.Stringer or
// an error), are quoted with quoteValue.
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return quoteValue(val)
	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", val)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val)
	case float32, float64:
		return fmt.Sprintf("%v", val)
	case bool:
		if val {
			return "1"
		}
		return "0"
	case nil:
		return "$NULL$"
	default:
		return quoteValue(fmt.Sprintf("%v", val))
	}
}

// Common operators for convenience.
const (
	OpEqual        = "="
	OpNotEqual     = "!="
	OpLessThan     = "<"
	OpLessEqual    = "<="
	OpGreaterThan  = ">"
	OpGreaterEqual = ">="
	OpLike         = "LIKE"
)
