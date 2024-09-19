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
	description = "Msg sends an example interactive messages."
	pluginName  = "msg"
)

// version is set via ldflags by GoReleaser.
var version = "dev"

// MsgExecutor implements the Botkube executor plugin interface.
type MsgExecutor struct {
	state map[string]map[string]string // State to keep track of selections
}

// triggerBotkubeCommand programmatically triggers a Botkube command.
func triggerBotkubeCommand(ctx context.Context, cmd string) (string, error) {
	// Prepare the input for the executor
	input := executor.ExecuteInput{
		Command: cmd,
	}

	// Simulate triggering command execution within Botkube's plugin architecture
	output, err := (&MsgExecutor{}).Execute(ctx, input)
	if err != nil {
		return "", err
	}

	// Assuming output is a plaintext message or code block
	return output.Message.BaseBody.CodeBlock, nil
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

	// Assume `in.Command` contains the action and value in a structured format
	action, value := parseCommand(in.Command)

	// Use a generic key for simplicity; adapt if needed
	sessionID := "default_session" // Replace with an actual identifier if available

	// Initialize session state if not already present
	if _, ok := e.state[sessionID]; !ok {
		e.state[sessionID] = make(map[string]string)
	}

	switch action {
	case "select_first":
		// Store the selection from the first dropdown
		e.state[sessionID]["first"] = value
		return showBothSelects(e.state[sessionID]["first"], e.state[sessionID]["second"]), nil
	case "select_second":
		// Store the selection from the second dropdown and show the button
		e.state[sessionID]["second"] = value
		return showBothSelects(e.state[sessionID]["first"], e.state[sessionID]["second"]), nil
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
				Plaintext: "Showcases interactive message capabilities. Please select an option from the first dropdown.",
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
											{Name: "pod", Value: "pod"},
											{Name: "deploy", Value: "deploy"},
											{Name: "cronjob", Value: "cronjob"},
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

// showBothSelects displays the second dropdown after the first one is selected and adds a "Run command" button if both selections are made.
func showBothSelects(firstSelection, secondSelection string) executor.ExecuteOutput {
	btnBuilder := api.NewMessageButtonBuilder()
	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

	// Initialize the sections array
	sections := []api.Section{
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
									{Name: "pod", Value: "pod"},
									{Name: "deploy", Value: "deploy"},
									{Name: "cronjob", Value: "cronjob"},
								},
							},
						},
						InitialOption: &api.OptionItem{
							Name:  firstSelection,
							Value: firstSelection,
						},
					},
					{
						Name:    "second",
						Command: cmdPrefix("select_second"),
						OptionGroups: []api.OptionGroup{
							{
								Name: "Second Group",
								Options: []api.OptionItem{
									{Name: "update-chrome-data-incentives-stack", Value: "update-chrome-data-incentives-stack"},
									{Name: "botkube", Value: "botkube"},
								},
							},
						},
						InitialOption: &api.OptionItem{
							Name:  "update-chrome-data-incentives-stack",
							Value: "update-chrome-data-incentives-stack",
						},
					},
				},
			},
		},
	}

	// Only add the button if both selections are made
	if firstSelection != "" && secondSelection != "" {
		code := fmt.Sprintf("kubectl get %s -n %s", firstSelection, secondSelection)
		command := fmt.Sprintf("kubectl get %s -n %s", firstSelection, secondSelection)

		// Programmatically trigger Botkube command execution
		ctx := context.Background()
		commandOutput, err := triggerBotkubeCommand(ctx, command)
		if err != nil {
			commandOutput = fmt.Sprintf("Failed to execute Botkube command: %s", err)
		}
		sections = append(sections, api.Section{
			Base: api.Base{
				Body: api.Body{
					CodeBlock: commandOutput,
				},
			},
			Buttons: []api.Button{
				btnBuilder.ForCommandWithoutDesc("Run command", code, api.ButtonStylePrimary),
			},
		})
	}

	return executor.ExecuteOutput{
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: "You've selected from the first dropdown. Now select from the second dropdown.",
			},
			Sections:          sections,
			OnlyVisibleForYou: true,
			ReplaceOriginal:   true,
		},
	}
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
