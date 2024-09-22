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
	Default     string   `json:"default,omitempty"`
	Type        string   `json:"type"`
}

type ScriptOutput struct {
	Options []Option `json:"options"`
}

// Helper function to run the shell script and get the JSON output
func runScript(scriptName string) (*ScriptOutput, error) {
	cmd := exec.Command("sh", fmt.Sprintf("/scripts/%s", scriptName), "--json-help")

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
// Execute returns a given command as a response.
func (e *MsgExecutor) Execute(_ context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {
	if !in.Context.IsInteractivitySupported {
		return executor.ExecuteOutput{
			Message: api.NewCodeBlockMessage("Interactivity for this platform is not supported", true),
		}, nil
	}

	// Parse the action and value from the command
	action, value := parseCommand(in.Command)

	sessionID := "default_session" // Replace with an actual identifier if available

	// Initialize session state if not already present
	if _, ok := e.state[sessionID]; !ok {
		e.state[sessionID] = make(map[string]string)
	}

	switch action {
	case "select_first":
		if e.state[sessionID]["first"] != value {
			for key := range e.state[sessionID] {
				delete(e.state[sessionID], key)
			}
		}

		// Store the selection from the first dropdown
		e.state[sessionID]["first"] = value
		return showBothSelects(e.state[sessionID]), nil

	case "select_dynamic":
		// Store dynamic dropdown selections (flag is passed in the command)
		flag := strings.Fields(value)[0]
		e.state[sessionID][flag] = strings.TrimPrefix(value, flag+" ")
		return showBothSelects(e.state[sessionID]), nil
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
		if !file.IsDir() && file.Name() != "..data" {
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
								Name:    "Job Name",
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

// showBothSelects dynamically generates dropdowns based on the selected options.
func showBothSelects(state map[string]string) executor.ExecuteOutput {
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
						Name:   "Job Name",
						Command: cmdPrefix("select_first"),
						OptionGroups: []api.OptionGroup{
							{
								Name:    "Job Name",
								Options: fileList,
							},
						},
						InitialOption: &api.OptionItem{
							Name:  state["first"], // Get first selection from state
							Value: state["first"],
						},
					},
				},
			},
		},
	}

	// Run the script to get dynamic options based on the first selection
	scriptOutput, err := runScript(state["first"])
	if err != nil {
		log.Fatalf("Error running script: %v", err)
	}

	// Create multiple dropdowns based on the options in the script output
	for _, option := range scriptOutput.Options {
		// Skip the help options (-h, --help)
		if option.Flags[0] == "-h" {
			continue
		}

		if option.Type == "bool" || option.Type == "dropdown" {

			var dropdownOptions []api.OptionItem
			boolValues := []string{"true", "false"}
			values := option.Values; if option.Type == "bool" { values = boolValues }
			for _, value := range values {
				dropdownOptions = append(dropdownOptions, api.OptionItem{
					Name:  value,
					Value: fmt.Sprintf("%s %s", option.Flags[0], value),
				})
			}

			// Construct the flag key for the state
			flagKey := fmt.Sprintf("%s-%s", state["first"], option.Flags[0])

			// Check if there's an InitialOption and update the state if it’s not already set
			if _, exists := state[flagKey]; !exists && option.Default != "" {
				state[flagKey] = fmt.Sprintf("%s %s", option.Flags[0], option.Default)
			}

			var initialOption *api.OptionItem
			if option.Default != "" {
				initialOption = &api.OptionItem{
					Name:  option.Default,
					Value: fmt.Sprintf("%s %s", option.Flags[0], option.Default),
				}
			} else {
				initialOption = nil // Set to nil if Default is not set
			}

			// Add the dropdown with the InitialOption if available
			sections[0].Selects.Items = append(sections[0].Selects.Items, api.Select{
				Name:    option.Description, // Adjust name based on flags
				Command: cmdPrefix(fmt.Sprintf("select_dynamic %s", flagKey)), // Handle dynamic dropdown
				OptionGroups: []api.OptionGroup{
					{
						Name:    option.Description,
						Options: dropdownOptions,
					},
				},
				InitialOption: initialOption,
			})
		}
		if option.Type == "text" {
			sections[0].Selects.Items = append(sections[0], {
				PlaintextInputs: api.LabelInputs{
					api.LabelInput{
						// Command:          "@BKTesting kubectl @builder --filter-query ",
						DispatchedAction: api.DispatchInputActionOnCharacter,
						Text:             "Filter output",
						Placeholder:      "Filter output by string (optional)",
					},
				})
			}
		}
	}

	// If all selections are made, show the run button
	if allSelectionsMade(state, scriptOutput.Options) {
		code := buildFinalCommand(state, scriptOutput.Options)
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
				Plaintext: "You've selected from the dropdowns. Now run the command if ready.",
			},
			Sections:          sections,
			OnlyVisibleForYou: true,
			ReplaceOriginal:   true,
		},
	}
}

// Helper function to check if all selections are made
func allSelectionsMade(state map[string]string, options []Option) bool {
	for _, option := range options {
		if option.Flags[0] == "-h" {
			continue
		}
		if state[fmt.Sprintf("%s-%s", state["first"], option.Flags[0])] == "" {
			return false
		}
	}
	return true
}

// Helper function to build the final command based on all selections
func buildFinalCommand(state map[string]string, options []Option) string {
	var commandParts []string

	// Add the first selection (e.g., job name)
	if first, ok := state["first"]; ok {
		commandParts = append(commandParts, first)
	}

	// Add options in the same order as they appear in the script output
	for _, option := range options {
		// Construct the key as used in the state map
		flagKey := fmt.Sprintf("%s-%s", state["first"], option.Flags[0])
		if value, ok := state[flagKey]; ok && value != "" {
			commandParts = append(commandParts, value)
		}
	}

	return fmt.Sprintf("run %s", strings.Join(commandParts, " "))
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
