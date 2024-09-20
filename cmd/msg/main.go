package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"log"
	"os/exec"
	"encoding/json"

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

// JSON structure for the script output
type Option struct {
	Flags       []string `json:"flags"`
	Description string   `json:"description"`
	Values      []string `json:"values,omitempty"`
	Default     bool     `json:"default,omitempty"`
}

type ScriptOutput struct {
	Options []Option `json:"options"`
}

// Helper function to run the shell script and get the JSON output
func runScript(scriptName string) (*ScriptOutput, error) {
	cmd := exec.Command("sh", "/scripts/update-chrome-data-incentives-stack", "--json-help")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run script: %v", err)
	}

	// Parse JSON output
	var result ScriptOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse script output: %v", err)
	}
	return &result, nil
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
	case "run_command":
		// Handle the command execution after both selections
		first := e.state[sessionID]["first"]
		second := e.state[sessionID]["second"]
		if first != "" && second != "" {
			return executor.ExecuteOutput{
				Message: api.NewCodeBlockMessage(fmt.Sprintf("Running command with: First = %s, Second = %s", first, second), true),
			}, nil
		}
		return executor.ExecuteOutput{
			Message: api.NewCodeBlockMessage("Both selections must be made before running the command.", true),
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

// getFileOptions retrieves file options from the /scripts directory.
func getFileOptions() ([]api.OptionItem, error) {
	dir := "/scripts"
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open directory: %v", err)
	}

	var fileList []api.OptionItem
	for _, file := range files {
		if !file.IsDir() {
			fileList = append(fileList, api.OptionItem{
				Name:  file.Name(),
				Value: file.Name(),
			})
		}
	}
	return fileList, nil
}

func initialMessages() executor.ExecuteOutput {
	fileList, err := getFileOptions()
	if err != nil {
		log.Fatalf("Error retrieving file options: %v", err)
	}

	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

	return executor.ExecuteOutput{
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: "Select a file from the dropdown.",
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
										Name:    "Job Name",
										Options: fileList, // Use the retrieved file list
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
	fileList, err := getFileOptions()
	if err != nil {
		log.Fatalf("Error retrieving file options: %v", err)
	}
	btnBuilder := api.NewMessageButtonBuilder()
	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

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
								Name:    "Job Name",
								Options: fileList,
							},
						},
						InitialOption: &api.OptionItem{
							Name:  firstSelection,
							Value: firstSelection,
						},
					},
				},
			},
		},
	}

	// Show second dropdown if the first selection is made
	if firstSelection != "" {

		// Run the script to get dynamic options
		scriptOutput, err := runScript(firstSelection)
		if err != nil {
			log.Fatalf("Error running script: %v", err)
		}
       // Create multiple dropdowns based on the options in the script output
        for _, option := range scriptOutput.Options {
            // var dropdownOptions []api.OptionItem
			// if option.Flags[0] != "-h" {
			// 	for _, value := range option.Values {
		    //         dropdownOptions = append(dropdownOptions, api.OptionItem{
		    //             Name:  value,
		    //             Value: value,
		    //         })
			// 	}
			// }

            // Add each dynamic dropdown to the section
			sections[0].Selects.Items = append(sections[0].Selects.Items, api.Select{
				Name:    "second",
				Command: cmdPrefix("select_second"),
				OptionGroups: []api.OptionGroup{
					{
						Name: "Second Group",
						Options: []api.OptionItem{
							{Name: "true", Value: "-i true"},
							{Name: "false", Value: "-i false"},
						},
					},
				},
			})
        }

		// sections[0].Selects.Items = append(sections[0].Selects.Items, api.Select{
		// 	Name:    "second",
		// 	Command: cmdPrefix("select_second"),
		// 	OptionGroups: []api.OptionGroup{
		// 		{
		// 			Name: "Second Group",
		// 			Options: []api.OptionItem{
		// 				{Name: "true", Value: "-i true"},
		// 				{Name: "false", Value: "-i false"},
		// 			},
		// 		},
		// 	},
		// })
    }



	// Only add the button if both selections are made
	if firstSelection != "" && secondSelection != "" {

		code := fmt.Sprintf("run %s %s", firstSelection, secondSelection)
		sections = append(sections, api.Section{
			Base: api.Base{
				Body: api.Body{
					CodeBlock: code,
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
