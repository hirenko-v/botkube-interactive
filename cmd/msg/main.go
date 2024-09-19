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
//
//nolint:gocritic  //hugeParam: in is heavy (80 bytes); consider passing it by pointer
func (MsgExecutor) Execute(_ context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {
	if !in.Context.IsInteractivitySupported {
		return executor.ExecuteOutput{
			Message: api.NewCodeBlockMessage("Interactivity for this platform is not supported", true),
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

func initialMessages() executor.ExecuteOutput {
	btnBuilder := api.NewMessageButtonBuilder()
	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

	return executor.ExecuteOutput{
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: "Showcases interactive message capabilities",
			},
	// Define a list of jobs
	jobs := []api.OptionItem{
		{Name: "Job1", Value: "job1"},
		{Name: "Job2", Value: "job2"},
		{Name: "Job3", Value: "job3"},
	}

	// Define a list of parameters
	params := []api.OptionItem{
		{Name: "Param1", Value: "param1"},
		{Name: "Param2", Value: "param2"},
		{Name: "Param3", Value: "param3"},
	}

	return executor.ExecuteOutput{
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: "Select a job and parameters, then click run:",
			},
			Sections: []api.Section{
				{
					Selects: api.Selects{
						ID: "job-dropdown",
						Items: []api.Select{
							{
								Name:    "Select Job",
								Command: cmdPrefix("select job"),
								OptionGroups: []api.OptionGroup{
									{
										Name: "Jobs",
										Options: jobs,
									},
								},
								InitialOption: &jobs[0], // Optional: Set an initial value
							},
						},
					},
				},
				{
					Selects: api.Selects{
						ID: "param-dropdown",
						Items: []api.Select{
							{
								Name:    "Select Parameters",
								Command: cmdPrefix("select param"),
								OptionGroups: []api.OptionGroup{
									{
										Name: "Parameters",
										Options: params,
									},
								},
								InitialOption: &params[0], // Optional: Set an initial value
							},
						},
					},
				},
				{
					Buttons: []api.Button{
						btnBuilder.ForCommandWithDescCmd("Run po", fmt.Sprintf("%s po", "kubectl get"), api.ButtonStylePrimary),
						btnBuilder.ForCommandWithoutDesc(
							"Run",
							fmt.Sprintf("kubectl run job ${job} ${param}"), // Command to be executed
						),
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
