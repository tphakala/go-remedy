package remedy

import (
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
