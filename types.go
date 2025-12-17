package remedy

// Entry represents a single entry (record) from a BMC Remedy form.
type Entry struct {
	Values map[string]any `json:"values"`
	Links  []Link         `json:"_links,omitzero"`
}

// EntryList represents a collection of entries returned from a list query.
type EntryList struct {
	Entries []Entry `json:"entries"`
	Links   []Link  `json:"_links,omitzero"`
}

// Link represents a HATEOAS link in API responses.
type Link struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

// Field represents metadata about a form field.
type Field struct {
	ID       int    `json:"fieldId"`
	Name     string `json:"fieldName"`
	DataType string `json:"dataType"`
}

// SortOrder defines the sort direction for queries.
type SortOrder string

const (
	// SortAsc sorts in ascending order.
	SortAsc SortOrder = "ASC"
	// SortDesc sorts in descending order.
	SortDesc SortOrder = "DESC"
)

// DeleteOption defines options for delete operations.
type DeleteOption string

const (
	// DeleteOptionNone performs a standard delete.
	DeleteOptionNone DeleteOption = "NONE"
	// DeleteOptionForce forces deletion even if entry is locked.
	DeleteOptionForce DeleteOption = "FORCE"
	// DeleteOptionNoCascade prevents cascade delete of related entries.
	DeleteOptionNoCascade DeleteOption = "NOCASCADE"
)

// apiErrorResponse represents the error format returned by BMC Remedy REST API.
type apiErrorResponse struct {
	MessageType         string `json:"messageType"`
	MessageText         string `json:"messageText"`
	MessageAppendedText string `json:"messageAppendedText"`
	MessageNumber       int    `json:"messageNumber"`
}

