package script

import (
	"errors"
	"fmt"
)

// UIKind identifies a scripted UI request without exposing renderer details.
type UIKind string

const (
	UIKindInput       UIKind = "input"
	UIKindTextarea    UIKind = "textarea"
	UIKindSelect      UIKind = "select"
	UIKindMultiSelect UIKind = "multiselect"
	UIKindConfirm     UIKind = "confirm"
	UIKindForm        UIKind = "form"
	UIKindActions     UIKind = "actions"
	UIKindViewer      UIKind = "viewer"
)

// ErrUnknownUIToken identifies a delayed or otherwise stale UI response.
var ErrUnknownUIToken = errors.New("unknown script UI token")

// UIRequest is a validated request yielded by a command coroutine.
type UIRequest struct {
	Token string
	Kind  UIKind
	Spec  UISpec
}

// UISpec is the declarative payload shared by scripted widgets. Fields that
// are not relevant to a particular Kind remain at their zero value.
type UISpec struct {
	Title        string
	Label        string
	Initial      any
	Placeholder  string
	Required     bool
	MinLength    int
	MaxLength    int
	Pattern      string
	PatternHint  string
	Searchable   bool
	InitialID    string
	InitialIDs   []string
	Message      string
	Detail       []string
	ConfirmLabel string
	RejectLabel  string
	Default      bool
	Destructive  bool
	Items        []UIItem
	Fields       []UIField
	Actions      []UIAction
	Content      string
	Format       string
	Wrap         bool
	LineNumbers  bool
}

type UIItem struct {
	ID             string
	Label          string
	Description    string
	Icon           string
	Disabled       bool
	DisabledReason string
	Group          string
}

type UIField struct {
	ID          string
	Type        string
	Label       string
	Description string
	Placeholder string
	Required    bool
	Initial     any
	Items       []UIItem
	MinLength   int
	MaxLength   int
	Pattern     string
	PatternHint string
}

type UIAction struct {
	ID                string
	Label             string
	Key               string
	Primary           bool
	Destructive       bool
	Disabled          bool
	DisabledReason    string
	RequiresSelection bool
}

type CursorPosition struct {
	Line   int
	Column int
	Offset int
}

type TextSelection struct {
	StartOffset int
	EndOffset   int
	Text        string
}

// UIResult is the common result envelope passed back into a suspended script.
type UIResult struct {
	Action    string
	Submitted bool
	Cancelled bool
	Value     any
	Values    map[string]any
	IDs       []string
	Cursor    *CursorPosition
	Selection *TextSelection
	Reason    string
}

func (r UIRequest) validate() error {
	switch r.Kind {
	case UIKindInput, UIKindTextarea, UIKindSelect, UIKindMultiSelect, UIKindConfirm, UIKindForm, UIKindActions, UIKindViewer:
		return nil
	default:
		return fmt.Errorf("unsupported kbrd.ui kind %q", r.Kind)
	}
}
