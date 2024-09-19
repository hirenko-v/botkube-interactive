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

	// Handle the initial command, show job selection
	if strings.TrimSpace(in.Command) == pluginName {
		return initialJobSelection(), nil
	}

	// Handle job selection
	if strings.HasPrefix(in.Command, "select job") {
		job := strings.TrimPrefix(in.Command, "select job ")
		return showParametersForJob(job), nil
	}

	// Handle button click (final execution) after both job and param selection
	if strings.HasPrefix(in.Command, "run job") {
		parts := strings.Split(in.Command, " ")
		if len(parts) < 4 {
			return executor.ExecuteOutput{
				Message: api.NewCodeBlockMessage("Error: Job or parameter missing", true),
			}, nil
		}
		job := parts[2]
		param := parts[3]

		// Final command execution with job and parameter
		return executor.ExecuteOutput{
			Message: api.NewCodeBlockMessage(fmt.Sprintf("Executing: kubectl run %s %s", job, param), true),
		}, nil
	}

	// Fallback if no command matched
	msg := fmt.Sprintf("Plain command: %s", in.Command)
	return executor.ExecuteOutput{
		Message: api.NewCodeBlockMessage(msg, true),
	}, nil
}

// initialJobSelection shows only the job dropdown
func initialJobSelection() executor.ExecuteOutput {
	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

	// Define a list of jobs
	jobs := []api.OptionItem{
		{Name: "Job1", Value: "job1"},
		{Name: "Job2", Value: "job2"},
		{Name: "Job3", Value: "job3"},
	}

	return executor.ExecuteOutput{
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: "Select a job:",
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
										Name:    "Jobs",
										Options: jobs,
									},
								},
								InitialOption: &jobs[0], // Optional: Set an initial value
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

// showParametersForJob shows the parameter dropdown after a job is selected
func showParametersForJob(job string) executor.ExecuteOutput {
	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
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
				Plaintext: fmt.Sprintf("You selected job: %s. Now select a parameter:", job),
			},
			Sections: []api.Section{
				{
					Selects: api.Selects{
						ID: "param-dropdown",
						Items: []api.Select{
							{
								Name:    "Select Parameter",
								Command: cmdPrefix(fmt.Sprintf("run job %s", job)),
								OptionGroups: []api.OptionGroup{
									{
										Name:    "Parameters",
										Options: params,
									},
								},
								InitialOption: &params[0], // Optional: Set an initial value
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
