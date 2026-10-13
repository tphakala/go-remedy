package remedy

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuery_And(t *testing.T) {
	tests := []struct {
		name     string
		build    func() string
		expected string
	}{
		{
			name: "single condition",
			build: func() string {
				return NewQuery().And("Status", "=", "Open").Build()
			},
			expected: `'Status' = "Open"`,
		},
		{
			name: "two AND conditions",
			build: func() string {
				return NewQuery().
					And("Status", "=", "Open").
					And("Priority", "<", 3).
					Build()
			},
			expected: `'Status' = "Open" AND 'Priority' < 3`,
		},
		{
			name: "integer value",
			build: func() string {
				return NewQuery().And("Priority", "=", 1).Build()
			},
			expected: `'Priority' = 1`,
		},
		{
			name: "boolean true",
			build: func() string {
				return NewQuery().And("Active", "=", true).Build()
			},
			expected: `'Active' = 1`,
		},
		{
			name: "boolean false",
			build: func() string {
				return NewQuery().And("Active", "=", false).Build()
			},
			expected: `'Active' = 0`,
		},
		{
			name: "nil value",
			build: func() string {
				return NewQuery().And("Field", "=", nil).Build()
			},
			expected: `'Field' = $NULL$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.build()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestQuery_Or(t *testing.T) {
	result := NewQuery().
		And("Status", "=", "Open").
		Or("Status", "=", "Pending").
		Build()

	expected := `'Status' = "Open" OR 'Status' = "Pending"`
	assert.Equal(t, expected, result)
}

func TestQuery_Raw(t *testing.T) {
	result := NewQuery().
		And("Status", "=", "Open").
		Raw("'Priority' < 3 OR 'Urgency' = \"High\"").
		Build()

	expected := `'Status' = "Open" AND ('Priority' < 3 OR 'Urgency' = "High")`
	assert.Equal(t, expected, result)
}

func TestQuery_Complex(t *testing.T) {
	result := NewQuery().
		And("Status", "=", "Open").
		And("Assignee", "!=", "").
		And("Priority", "<=", 2).
		Build()

	expected := `'Status' = "Open" AND 'Assignee' != "" AND 'Priority' <= 2`
	assert.Equal(t, expected, result)
}

func TestQuery_Empty(t *testing.T) {
	result := NewQuery().Build()
	assert.Empty(t, result)
}

func TestQuery_FloatValue(t *testing.T) {
	result := NewQuery().And("Score", ">", 3.14).Build()
	assert.Equal(t, `'Score' > 3.14`, result)
}

func TestQuery_FieldNameWithSingleQuote_IsEscaped(t *testing.T) {
	// Field names containing single quotes must be escaped to prevent injection
	// Input: Status' OR '1'='1
	// Without escaping: 'Status' OR '1'='1' = "value" (injection!)
	// With escaping: 'Status'' OR ''1''=''1' = "value" (safe, literal field name)
	result := NewQuery().And("Status' OR '1'='1", "=", "value").Build()

	// The field name should have its single quotes escaped by doubling them
	assert.Equal(t, `'Status'' OR ''1''=''1' = "value"`, result)
}

func TestQuery_StringValueWithDoubleQuote_IsEscaped(t *testing.T) {
	// A value must stay inside one string literal. The embedded double quotes
	// are doubled, which keeps the value inside one literal under the doubling rule.
	result := NewQuery().And("Status", OpEqual, `x" OR 'Status' = "y`).Build()

	assert.Equal(t, `'Status' = "x"" OR 'Status' = ""y"`, result)
}

// stringerValue is a non-string type whose String method returns its text,
// so formatValue sends it through the fallback branch.
type stringerValue string

func (s stringerValue) String() string { return string(s) }

func TestFormatValue_String(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain", "Open", `"Open"`},
		{"empty", "", `""`},
		{"embedded double quote", `a"b`, `"a""b"`},
		{"lone double quote", `"`, `""""`},
		{"two double quotes in a row", `""`, `""""""`},
		{"three double quotes in a row", `"""`, `""""""""`},
		{"BMC doc example", `Robert "Bobby" Smith`, `"Robert ""Bobby"" Smith"`},
		{"backslash unchanged", `a\b`, `"a\b"`},
		{"trailing backslash unchanged", `a\`, `"a\"`},
		{"backslash before quote", `a\"b`, `"a\""b"`},
		{"newline unchanged", "a\nb", "\"a\nb\""},
		{"tab unchanged", "a\tb", "\"a\tb\""},
		{"printable non-ASCII", "äö 😀", "\"äö 😀\""},
		{"non-printable rune", "a\u200bb", "\"a\u200bb\""},
		{"invalid UTF-8", "a\xffb", "\"a\xffb\""},
		{"single quotes untouched", `O'Brien`, `"O'Brien"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatValue(tt.input))
		})
	}
}

func TestFormatValue_Fallback(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"stringer with quotes", stringerValue(`say "hi"`), `"say ""hi"""`},
		{"stringer with backslash", stringerValue(`C:\tmp`), `"C:\tmp"`},
		{"error with quotes", errors.New(`bad "x"`), `"bad ""x"""`},
		{"plain stringer", stringerValue("Open"), `"Open"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatValue(tt.input))
		})
	}
}

func TestQuery_InvalidOperator_ReturnsError(t *testing.T) {
	// Invalid operators should cause an error when building the query
	q := NewQuery().AndSafe("Status", "=; DROP TABLE", "Open")
	result, err := q.BuildSafe()

	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "invalid operator")
}

func TestQuery_ValidOperators(t *testing.T) {
	validOps := []string{"=", "!=", "<", "<=", ">", ">=", "LIKE"}

	for _, op := range validOps {
		t.Run(op, func(t *testing.T) {
			q := NewQuery().AndSafe("Field", op, "value")
			result, err := q.BuildSafe()

			require.NoError(t, err)
			assert.NotEmpty(t, result)
		})
	}
}

func TestQuery_FieldNameWithSpecialChars(t *testing.T) {
	// AR System allows various characters in field names
	// Spaces, colons, parentheses are common
	tests := []struct {
		field    string
		expected string
	}{
		{"HPD:Help Desk", `'HPD:Help Desk' = "test"`},
		{"Status (Reason)", `'Status (Reason)' = "test"`},
		{"Field-Name_123", `'Field-Name_123' = "test"`},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			result := NewQuery().And(tt.field, "=", "test").Build()
			assert.Equal(t, tt.expected, result)
		})
	}
}
