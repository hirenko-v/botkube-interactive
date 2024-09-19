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
	selectedJob  string
	selectedParam string
}

// Metadata returns details about the Msg plugin.
func (MsgExecutor) Metadata(context.Context) (api.MetadataOutput, error) {
	return api.MetadataOutput{
		Version:     version,
		Description: description,
	}, nil
}

// Execute handles the command execution logic.
func (m *MsgExecutor) Execute(_ context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {
	if !in.Context.IsInteractivitySupported {
		return executor.ExecuteOutput{
			Message: api.NewCodeBlockMessage("Interactivity for this platform is not supported", true),
		}, nil
	}

	// Initial command to show job selection
	if strings.TrimSpace(in.Command) == pluginName {
		return m.initialJobSelection(), nil
	}

	// Handle job selection
	if strings.HasPrefix(in.Command, "select job") {
		m.selectedJob = strings.TrimPrefix(in.Command, "select job ")
		return m.showParametersForJob(), nil
	}

	// Handle parameter selection
	if strings.HasPrefix(in.Command, "select param") {
		m.selectedParam = strings.TrimPrefix(in.Command, "select param ")
		return m.showRunButton(), nil
	}

	// Handle final execution of the command
	if strings.HasPrefix(in.Command, "run job") {
		if m.selectedJob == "" || m.selectedParam == "" {
			return executor.ExecuteOutput{
				Message: api.NewCodeBlockMessage("Error: Job or parameter missing", true),
			}, nil
		}

		// Execute final kubectl run command
		return executor.ExecuteOutput{
			Message: api.NewCodeBlockMessage(fmt.Sprintf("Executing: kubectl run %s %s", m.selectedJob, m.selectedParam), true),
		}, nil
	}

	// Fallback for unmatched commands
	msg := fmt.Sprintf("Plain command: %s", in.Command)
	return executor.ExecuteOutput{
		Message: api.NewCodeBlockMessage(msg, true),
	}, nil
}

// initialJobSelection shows only the job dropdown
func (m *MsgExecutor) initialJobSelection() executor.ExecuteOutput {
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
func (m *MsgExecutor) showParametersForJob() executor.ExecuteOutput {
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
				Plaintext: fmt.Sprintf("You selected job: %s. Now select a parameter:", m.selectedJob),
			},
			Sections: []api.Section{
				{
					Selects: api.Selects{
						ID: "param-dropdown",
						Items: []api.Select{
							{
								Name:    "Select Parameter",
								Command: cmdPrefix("select param"),
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

// showRunButton shows the "Run" button after both job and parameter are selected
func (m *MsgExecutor) showRunButton() executor.ExecuteOutput {
	btnBuilder := api.NewMessageButtonBuilder()
	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

	return executor.ExecuteOutput{
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: fmt.Sprintf("You selected job: %s and parameter: %s.", m.selectedJob, m.selectedParam),
			},
			Sections: []api.Section{
				{
					Buttons: []api.Button{
						btnBuilder.ForCommandWithDescCmd("Run", cmdPrefix(fmt.Sprintf("run job %s %s", m.selectedJob, m.selectedParam)), api.ButtonStylePrimary),
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
