package mcp

import (
	"strings"
	"testing"
)

func TestChoiceSchemaUsesTitledValues(t *testing.T) {
	schema, err := choiceSchema([]elicitationChoice{
		{Value: "/boards/one", Title: "Work — /boards/one"},
		{Value: "/boards/two", Title: "Work — /boards/two"},
	})
	if err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	choice := properties["choice"].(map[string]any)
	oneOf := choice["oneOf"].([]map[string]any)
	if len(oneOf) != 2 {
		t.Fatalf("oneOf = %+v", oneOf)
	}
	if oneOf[0]["const"] != "/boards/one" || oneOf[0]["title"] != "Work — /boards/one" {
		t.Fatalf("first titled choice = %+v", oneOf[0])
	}
}

func TestChoiceSchemaRejectsInvalidChoices(t *testing.T) {
	for _, tc := range []struct {
		name    string
		choices []elicitationChoice
		want    string
	}{
		{name: "empty list", want: "at least one"},
		{name: "empty value", choices: []elicitationChoice{{Title: "Empty"}}, want: "cannot be empty"},
		{name: "duplicate", choices: []elicitationChoice{{Value: "x"}, {Value: "x"}}, want: "duplicate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := choiceSchema(tc.choices)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
