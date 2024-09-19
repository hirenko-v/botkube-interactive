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
type MsgExecutor struct{}

// Metadata returns details about the Msg plugin.
func (MsgExecutor) Metadata(context.Context) (api.MetadataOutput, error) {
	return api.MetadataOutput{
		Version:     version,
		Description: description,
	}, nil
}

// Execute returns a given command as a response.
func (MsgExecutor) Execute(_ context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {
	if !in.Context.IsInteractivitySupported {
		return executor.ExecuteOutput{
			Message: api.NewCodeBlockMessage("Interactivity for this platform is not supported", true),
		}, nil
	}

	// Handle command based on user selection
	commandParts := strings.Fields(in.Command)
	if len(commandParts) > 2 && commandParts[1] == "selects" {
		switch commandParts[2] {
		case "first":
			// First option is selected, respond appropriately
			return executor.ExecuteOutput{
				Message: api.NewCodeBlockMessage(fmt.Sprintf("You selected: %s", commandParts[3]), true),
			}, nil
		case "second":
			// Second option is selected, handle the final selection
			return executor.ExecuteOutput{
				Message: api.NewCodeBlockMessage(fmt.Sprintf("You selected: %s", commandParts[3]), true),
			}, nil
		}
	}

	if strings.TrimSpace(in.Command) == pluginName {
		return initialMessages(), nil
	}

	msg := fmt.Sprintf("Plain command: %s", in.Command)
	return executor.ExecuteOutput{
		Message: api.NewCodeBlockMessage(msg, true),
	}, nil
}

// initialMessages shows both selects side by side.
func initialMessages() executor.ExecuteOutput {
	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

	return executor.ExecuteOutput{
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: "Showcases interactive message capabilities",
			},
			Sections: []api.Section{
				{
					Selects: api.Selects{
						ID: "select-id-1",
						Items: []api.Select{
							{
								Name:    "first",
								Command: cmdPrefix("selects first"),
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
				{
					Selects: api.Selects{
						ID: "select-id-2",
						Items: []api.Select{
							{
								Name:    "second",
								Command: cmdPrefix("selects second"),
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
									Name:  "Option A",
									Value: "Option A",
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

func (MsgExecutor) Help(context.Context) (api.Message, error) {
	msg := description
	msg += fmt.Sprintf("\nJust type `%s %s`", api.MessageBotNamePlaceholder, pluginName)

	return api.NewPlaintextMessage(msg, false), nil
}

func main() {
	executor.Serve(map[string]plugin.Plugin{
		pluginName: &executor.Plugin{
			Executor: &MsgExecutor{},
		},
	})
}
