//go:build darwin

package reminders

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"kbrd/config"
)

//go:embed reminders.js
var remindersScript string

type appleScriptStore struct{}

type scriptRequest struct {
	Op         string            `json:"op"`
	Account    string            `json:"account,omitempty"`
	List       string            `json:"list"`
	CreateList bool              `json:"create_list,omitempty"`
	Operations []RemoteOperation `json:"operations,omitempty"`
}

type scriptResponse struct {
	Reminders []Reminder `json:"reminders,omitempty"`
}

func newPlatformStore() Store { return appleScriptStore{} }

func (appleScriptStore) Fetch(ctx context.Context, cfg config.RemindersConfig, create bool) ([]Reminder, error) {
	var response scriptResponse
	if err := runRemindersScript(ctx, scriptRequest{Op: "fetch", Account: cfg.Account, List: cfg.List, CreateList: create}, &response); err != nil {
		return nil, err
	}
	return response.Reminders, nil
}

func (appleScriptStore) Apply(ctx context.Context, cfg config.RemindersConfig, operations []RemoteOperation) ([]Reminder, error) {
	if len(operations) == 0 {
		return nil, nil
	}
	var response scriptResponse
	if err := runRemindersScript(ctx, scriptRequest{Op: "apply", Account: cfg.Account, List: cfg.List, Operations: operations}, &response); err != nil {
		return nil, err
	}
	return response.Reminders, nil
}

func runRemindersScript(ctx context.Context, request scriptRequest, response any) error {
	input, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode Reminders request: %w", err)
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", remindersScript)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("Reminders automation failed: %s", detail)
	}
	if response == nil {
		return nil
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), response); err != nil {
		return fmt.Errorf("decode Reminders response: %w (output %q)", err, strings.TrimSpace(stdout.String()))
	}
	return nil
}
