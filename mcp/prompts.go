package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"kbrd/board"
)

const boardPromptsFile = ".kbrd_mcp_prompts.yml"

var promptIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type promptDefinition struct {
	Name        string                     `yaml:"name"`
	Title       string                     `yaml:"title"`
	Description string                     `yaml:"description"`
	Arguments   []promptArgumentDefinition `yaml:"arguments"`
	Content     string                     `yaml:"content"`
	Messages    []promptMessageDefinition  `yaml:"messages"`
	board       board.Ref
}

type promptArgumentDefinition struct {
	Name        string `yaml:"name"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

type promptMessageDefinition struct {
	Role    string `yaml:"role"`
	Content string `yaml:"content"`
}

type promptsFile struct {
	Prompts []promptDefinition `yaml:"prompts"`
}

func registerPrompts(s *mcp.Server) {
	registerBuiltInPrompts(s)

	definitions, warnings, err := loadBoardPrompts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: load MCP prompts: %v\n", err)
		return
	}
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "warning: MCP prompts: %s\n", warning)
	}
	for _, definition := range definitions {
		definition := definition
		s.AddPrompt(definition.mcpPrompt(), definition.handler())
	}
}

func registerBuiltInPrompts(s *mcp.Server) {
	addBuiltInPrompt(s, "board_summary", "Summarize a board", "Inspect a board and summarize its current state.", []*mcp.PromptArgument{
		{Name: "board", Description: "Friendly name of the board", Required: true},
		{Name: "column", Description: "Optional column to summarize"},
		{Name: "focus", Description: "Optional area to emphasize"},
	}, func(args map[string]string) string {
		scope := promptColumnScope(args["column"])
		focus := optionalPromptClause(args["focus"], " Pay particular attention to %s.")
		return fmt.Sprintf("Inspect the kbrd board %q%s using show_board and summarize its current work, blockers, and likely next actions.%s Do not modify the board.", args["board"], scope, focus)
	})

	addBuiltInPrompt(s, "board_triage", "Triage a board", "Review a board and propose a prioritized triage plan.", []*mcp.PromptArgument{
		{Name: "board", Description: "Friendly name of the board", Required: true},
		{Name: "column", Description: "Optional column to triage"},
	}, func(args map[string]string) string {
		return fmt.Sprintf("Triage the kbrd board %q%s. Inspect it with show_board, identify stale, blocked, duplicate, or unclear cards, then propose a prioritized cleanup plan. Do not change cards or columns until the user approves the plan.", args["board"], promptColumnScope(args["column"]))
	})

	addBuiltInPrompt(s, "plan_board_work", "Plan work on a board", "Turn a goal into an actionable board plan.", []*mcp.PromptArgument{
		{Name: "board", Description: "Friendly name of the board", Required: true},
		{Name: "column", Description: "Optional column in which to plan the work"},
		{Name: "goal", Description: "Outcome to plan", Required: true},
	}, func(args map[string]string) string {
		return fmt.Sprintf("Plan work for the goal %q on the kbrd board %q%s. Inspect the existing board first, reuse relevant cards, and propose a concise set of new or updated cards with suggested columns and dependencies. Do not modify the board until the user approves the plan.", args["goal"], args["board"], promptColumnScope(args["column"]))
	})
}

func addBuiltInPrompt(s *mcp.Server, name, title, description string, arguments []*mcp.PromptArgument, render func(map[string]string) string) {
	prompt := &mcp.Prompt{Name: name, Title: title, Description: description, Arguments: arguments}
	s.AddPrompt(prompt, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		args, err := validatedPromptArguments(req, arguments)
		if err != nil {
			return nil, err
		}
		return textPromptResult(description, render(args)), nil
	})
}

func optionalPromptClause(value, format string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return fmt.Sprintf(format, value)
}

func promptColumnScope(column string) string {
	if strings.TrimSpace(column) == "" {
		return ""
	}
	return fmt.Sprintf(", limited to column %q", column)
}

func completePromptArgument(req *mcp.CompleteRequest) ([]string, error) {
	if req.Params.Argument.Name == "board" {
		switch req.Params.Ref.Name {
		case "board_summary", "board_triage", "plan_board_work":
			return completableBoardNames()
		}
		return nil, nil
	}
	if req.Params.Argument.Name != "column" {
		return nil, nil
	}

	switch req.Params.Ref.Name {
	case "board_summary", "board_triage", "plan_board_work":
		return completableColumns(completionContextArgument(req, "board"))
	}

	definitions, _, err := loadBoardPrompts()
	if err != nil {
		return nil, err
	}
	for _, definition := range definitions {
		if definition.registeredName() != req.Params.Ref.Name || !definition.hasArgument("column") {
			continue
		}
		columns, err := board.Columns(definition.board.Path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("list columns for prompt completion: %w", err)
		}
		return columns, nil
	}
	return nil, nil
}

func (definition promptDefinition) hasArgument(name string) bool {
	return slices.ContainsFunc(definition.Arguments, func(argument promptArgumentDefinition) bool {
		return argument.Name == name
	})
}

func loadBoardPrompts() ([]promptDefinition, []string, error) {
	refs, err := board.ListBoards()
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(refs, func(a, b board.Ref) int {
		return strings.Compare(a.Path, b.Path)
	})

	var definitions []promptDefinition
	var warnings []string
	seen := make(map[string]string)
	for _, ref := range refs {
		path := filepath.Join(ref.Path, boardPromptsFile)
		loaded, fileWarnings, err := readBoardPromptsFile(path, ref)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		warnings = append(warnings, fileWarnings...)
		for _, definition := range loaded {
			name := definition.registeredName()
			if previous, ok := seen[name]; ok {
				warnings = append(warnings, fmt.Sprintf("%s: prompt name %q collides with %s; skipped", path, name, previous))
				continue
			}
			seen[name] = path
			definitions = append(definitions, definition)
		}
	}
	return definitions, warnings, nil
}

func readBoardPromptsFile(path string, ref board.Ref) ([]promptDefinition, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	var file promptsFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, []string{fmt.Sprintf("%s: parse error: %v", path, err)}, nil
	}

	var valid []promptDefinition
	var warnings []string
	seen := make(map[string]bool)
	for i, definition := range file.Prompts {
		definition.board = ref
		if err := validatePromptDefinition(definition); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: entry %d (%q): %v", path, i, definition.Name, err))
			continue
		}
		if seen[definition.Name] {
			warnings = append(warnings, fmt.Sprintf("%s: duplicate prompt %q; later entry skipped", path, definition.Name))
			continue
		}
		seen[definition.Name] = true
		valid = append(valid, definition)
	}
	return valid, warnings, nil
}

func validatePromptDefinition(definition promptDefinition) error {
	if !promptIdentifier.MatchString(definition.Name) {
		return fmt.Errorf("name must contain only letters, digits, underscores, or hyphens")
	}
	if definition.Content == "" && len(definition.Messages) == 0 {
		return fmt.Errorf("content or messages is required")
	}
	if definition.Content != "" && len(definition.Messages) > 0 {
		return fmt.Errorf("content and messages are mutually exclusive")
	}
	seen := make(map[string]bool)
	for _, argument := range definition.Arguments {
		if !promptIdentifier.MatchString(argument.Name) {
			return fmt.Errorf("argument name %q must contain only letters, digits, underscores, or hyphens", argument.Name)
		}
		if argument.Name == "boardName" || argument.Name == "boardPath" {
			return fmt.Errorf("argument name %q is reserved", argument.Name)
		}
		if seen[argument.Name] {
			return fmt.Errorf("duplicate argument %q", argument.Name)
		}
		seen[argument.Name] = true
	}
	for _, message := range definition.effectiveMessages() {
		if message.Role != "user" && message.Role != "assistant" {
			return fmt.Errorf("message role %q must be user or assistant", message.Role)
		}
		if message.Content == "" {
			return fmt.Errorf("message content is required")
		}
		if _, err := template.New(definition.Name).Option("missingkey=error").Parse(message.Content); err != nil {
			return fmt.Errorf("parse message template: %w", err)
		}
	}
	return nil
}

func (definition promptDefinition) effectiveMessages() []promptMessageDefinition {
	if definition.Content != "" {
		return []promptMessageDefinition{{Role: "user", Content: definition.Content}}
	}
	return definition.Messages
}

func (definition promptDefinition) registeredName() string {
	return promptNamePart(definition.board.Label()) + "__" + definition.Name
}

func promptNamePart(value string) string {
	var result strings.Builder
	underscore := false
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			result.WriteRune(r)
			underscore = false
			continue
		}
		if result.Len() > 0 && !underscore {
			result.WriteByte('_')
			underscore = true
		}
	}
	part := strings.Trim(result.String(), "_")
	if part == "" {
		return "board"
	}
	return part
}

func (definition promptDefinition) mcpPrompt() *mcp.Prompt {
	arguments := make([]*mcp.PromptArgument, 0, len(definition.Arguments))
	for _, argument := range definition.Arguments {
		arguments = append(arguments, &mcp.PromptArgument{
			Name: argument.Name, Title: argument.Title, Description: argument.Description, Required: argument.Required,
		})
	}
	title := definition.Title
	if title == "" {
		title = definition.Name
	}
	title += " (" + definition.board.Label() + ")"
	return &mcp.Prompt{Name: definition.registeredName(), Title: title, Description: definition.Description, Arguments: arguments}
}

func (definition promptDefinition) handler() mcp.PromptHandler {
	return func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		arguments := make([]*mcp.PromptArgument, 0, len(definition.Arguments))
		for _, argument := range definition.Arguments {
			arguments = append(arguments, &mcp.PromptArgument{Name: argument.Name, Required: argument.Required})
		}
		values, err := validatedPromptArguments(req, arguments)
		if err != nil {
			return nil, err
		}
		values["boardName"] = definition.board.Label()
		values["boardPath"] = definition.board.Path

		messages := make([]*mcp.PromptMessage, 0, len(definition.effectiveMessages()))
		for _, message := range definition.effectiveMessages() {
			tmpl, err := template.New(definition.Name).Option("missingkey=error").Parse(message.Content)
			if err != nil {
				return nil, err
			}
			var rendered bytes.Buffer
			if err := tmpl.Execute(&rendered, values); err != nil {
				return nil, fmt.Errorf("render prompt %q: %w", definition.registeredName(), err)
			}
			messages = append(messages, &mcp.PromptMessage{Role: mcp.Role(message.Role), Content: &mcp.TextContent{Text: rendered.String()}})
		}
		return &mcp.GetPromptResult{Description: definition.Description, Messages: messages}, nil
	}
}

func validatedPromptArguments(req *mcp.GetPromptRequest, definitions []*mcp.PromptArgument) (map[string]string, error) {
	provided := map[string]string{}
	if req != nil && req.Params != nil && req.Params.Arguments != nil {
		provided = req.Params.Arguments
	}
	values := make(map[string]string, len(definitions))
	known := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		known[definition.Name] = true
		value := provided[definition.Name]
		if definition.Required && strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("prompt argument %q is required", definition.Name)
		}
		values[definition.Name] = value
	}
	for name := range provided {
		if !known[name] {
			return nil, fmt.Errorf("unknown prompt argument %q", name)
		}
	}
	return values, nil
}

func textPromptResult(description, text string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{
		Description: description,
		Messages:    []*mcp.PromptMessage{{Role: mcp.Role("user"), Content: &mcp.TextContent{Text: text}}},
	}
}
