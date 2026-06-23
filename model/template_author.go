package model

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"kbrd/board"
	"kbrd/template"
)

type columnTemplateCreated struct {
	FileName string
	Path     string
}

func createColumnTemplate(columnPath string, values templateAuthorValues) (columnTemplateCreated, error) {
	name := strings.TrimSpace(values.Name)
	if name == "" {
		return columnTemplateCreated{}, fmt.Errorf("template name cannot be empty")
	}
	filename := strings.TrimSpace(values.Filename)
	if filename == "" {
		return columnTemplateCreated{}, fmt.Errorf("template filename pattern cannot be empty")
	}
	body := strings.TrimSpace(values.Body)
	if body == "" {
		return columnTemplateCreated{}, fmt.Errorf("template body cannot be empty")
	}

	fileBase, err := board.SanitizeName(template.Slugify(name))
	if err != nil {
		return columnTemplateCreated{}, fmt.Errorf("invalid template name: %w", err)
	}
	dir := filepath.Join(columnPath, template.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return columnTemplateCreated{}, fmt.Errorf("failed to create template directory: %w", err)
	}

	fileName := fileBase + ".md"
	path := filepath.Join(dir, fileName)
	content := renderColumnTemplateStarter(name, filename, body)
	if err := writeNewColumnTemplate(path, content); err != nil {
		if errors.Is(err, os.ErrExist) {
			return columnTemplateCreated{}, fmt.Errorf("file already exists: %s", fileName)
		}
		return columnTemplateCreated{}, err
	}
	if _, err := template.Parse(path); err != nil {
		_ = os.Remove(path)
		return columnTemplateCreated{}, fmt.Errorf("invalid generated template: %w", err)
	}
	return columnTemplateCreated{FileName: fileName, Path: path}, nil
}

func writeNewColumnTemplate(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return err
		}
		return fmt.Errorf("failed to create template: %w", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("failed to write template: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("failed to write template: %w", err)
	}
	return nil
}

func renderColumnTemplateStarter(name, filename, body string) string {
	return fmt.Sprintf(`---
name: %s
filename: %s
steps:
  - title: Basics
    fields:
      - key: title
        type: input
        title: Title
        required: true
---
%s
`, strconv.Quote(name), strconv.Quote(filename), strings.TrimSpace(body))
}
