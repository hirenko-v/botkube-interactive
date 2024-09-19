package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/go-plugin"
	"github.com/kubeshop/botkube/pkg/api"
	"github.com/kubeshop/botkube/pkg/api/executor"
)

const (
	description = "Msg sends an example interactive message."
	pluginName  = "msg"
)

// version is set via ldflags by GoReleaser.
var version = "dev"

// MsgExecutor implements the Botkube executor plugin interface.
type MsgExecutor struct {
	state map[string]map[string]string // State to keep track of selections
}

// Metadata returns details about the Msg plugin.
func (MsgExecutor) Metadata(context.Context) (api.MetadataOutput, error) {
	return api.MetadataOutput{
		Version:     version,
		Description: description,
	}, nil
}

// Execute returns a given command as a response.
func (e *MsgExecutor) Execute(_ context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {
	if !in.Context.IsInteractivitySupported {
		return executor.ExecuteOutput{
			Message: api.NewCodeBlockMessage("Interactivity for this platform is not supported", true),
		}, nil
	}

	action, value := parseCommand(in.Command)

	// Use a generic key for simplicity; adapt if needed
	sessionID := "default_session" // Replace with an actual identifier if available

	// Initialize session state if not already present
	if _, ok := e.state[sessionID]; !ok {
		e.state[sessionID] = make(map[string]string)
	}

	switch action {
	case "select_first":
		e.state[sessionID]["first"] = value
		return updateDropdowns(e.state[sessionID]), nil
	case "select_second":
		e.state[sessionID]["second"] = value
		return updateDropdowns(e.state[sessionID]), nil
	case "run_command":
		// Execute the command or handle it
		return executor.ExecuteOutput{
			Message: api.NewCodeBlockMessage(fmt.Sprintf("Executing command:\n%s", e.state[sessionID]["full_command"]), true),
		}, nil
	}

	if strings.TrimSpace(in.Command) == pluginName {
		return initialMessages(), nil
	}

	msg := fmt.Sprintf("Plain command: %s", in.Command)
	return executor.ExecuteOutput{
		Message: api.NewCodeBlockMessage(msg, true),
	}, nil
}

// parseCommand parses the input command into action and value
func parseCommand(cmd string) (action, value string) {
	parts := strings.Fields(cmd)
	if len(parts) > 1 {
		action = parts[1]
		value = strings.Join(parts[2:], " ")
	}
	return
}

// initialMessages shows only the first dropdown.
func initialMessages() executor.ExecuteOutput {
	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

	return executor.ExecuteOutput{
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: "Please select an option from the first dropdown.",
			},
			Sections: []api.Section{
				{
					Selects: api.Selects{
						ID: "select-id-1",
						Items: []api.Select{
							{
								Name:    "first",
								Command: cmdPrefix("select_first"),
								OptionGroups: []api.OptionGroup{
									{
										Name: "Group 1",
										Options: []api.OptionItem{
											{Name: "BAR", Value: "BAR"},
											{Name: "BAZ", Value: "BAZ"},
											{Name: "XYZ", Value: "XYZ"},
										},
									},
								},
							},
						},
					},
				},
			},
			OnlyVisibleForYou: true,
			ReplaceOriginal:   false,
		},
	}
}

// updateDropdowns displays the updated dropdowns and command preview.
func updateDropdowns(selections map[string]string) executor.ExecuteOutput {
	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

	fullCommand := buildFullCommand(selections)
	return executor.ExecuteOutput{
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: fullCommand,
			},
			Sections: []api.Section{
				{
					Selects: api.Selects{
						ID: "select-id",
						Items: []api.Select{
							{
								Name:    "first",
								Command: cmdPrefix("select_first"),
								OptionGroups: []api.OptionGroup{
									{
										Name: "Group 1",
										Options: []api.OptionItem{
											{Name: "BAR", Value: "BAR"},
											{Name: "BAZ", Value: "BAZ"},
											{Name: "XYZ", Value: "XYZ"},
										},
									},
								},
								InitialOption: &api.OptionItem{
									Name:  selections["first"],
									Value: selections["first"],
								},
							},
							{
								Name:    "second",
								Command: cmdPrefix("select_second"),
								OptionGroups: []api.OptionGroup{
									{
										Name: "Second Group",
										Options: []api.OptionItem{
											{Name: "Option A", Value: "Option A"},
											{Name: "Option B", Value: "Option B"},
										},
									},
								},
								InitialOption: &api.OptionItem{
									Name:  selections["second"],
									Value: selections["second"],
								},
							},
						},
					},
				},
				{
					Buttons: []api.Button{
						{
							Name:    "Run command",
							Command: fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, "run_command"),
						},
					},
				},
			},
			OnlyVisibleForYou: true,
			ReplaceOriginal:   true,
		},
	}
}

// buildFullCommand constructs the full command string from selections
func buildFullCommand(selections map[string]string) string {
	cmd := "Command to execute:"
	if first, ok := selections["first"]; ok {
		cmd += fmt.Sprintf(" First Dropdown: %s", first)
	}
	if second, ok := selections["second"]; ok {
		cmd += fmt.Sprintf(" Second Dropdown: %s", second)
	}
	return cmd
}

func (MsgExecutor) Help(context.Context) (api.Message, error) {
	msg := description
	msg += fmt.Sprintf("\nJust type `%s %s`", api.MessageBotNamePlaceholder, pluginName)

	return api.NewPlaintextMessage(msg, false), nil
}

func main() {
	executor.Serve(map[string]plugin.Plugin{
		pluginName: &executor.Plugin{
			Executor: &MsgExecutor{
				state: make(map[string]map[string]string),
			},
		},
	})
}
