package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	go_plugin "github.com/hashicorp/go-plugin"
	"github.com/kubeshop/botkube/pkg/api"
	"github.com/kubeshop/botkube/pkg/api/executor"
	"github.com/kubeshop/botkube/pkg/plugin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	description = "Run Job."
	pluginName  = "job"
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
func (e *MsgExecutor) Execute(ctx context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {

	// Kubernetes client setup
	kubeConfigPath, deleteFn, err := plugin.PersistKubeConfig(ctx, in.Context.KubeConfig)
	if err != nil {
		log.Fatalf("Error writing kubeconfig file: %v", err)
	}
	defer func() {
		if deleteErr := deleteFn(ctx); deleteErr != nil {
			fmt.Fprintf(os.Stderr, "failed to delete kubeconfig file %s: %v", kubeConfigPath, deleteErr)
		}
	}()
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	}

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
		return initialMessages(ctx, clientset), nil
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

// Helper function to create the selects for the job name
func createJobNameSelect(fileList []api.OptionItem, initialOption *api.OptionItem, cmdPrefix string) api.Selects {
	return api.Selects{
		ID: "select-id-1",
		Items: []api.Select{
			{
				Name:    "Job Name",
				Command: cmdPrefix,
				OptionGroups: []api.OptionGroup{
					{
						Name:    "Job Name",
						Options: fileList, // Use the retrieved file list
					},
				},
				InitialOption: initialOption, // Set initial option if available
			},
		},
	}
}

func initialMessages(ctx context.Context, clientset *kubernetes.Clientset) executor.ExecuteOutput {

	// Get the list of namespaces
	namespaceList, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Fatalf("Error retrieving namespaces: %v", err)
	}

	// Format the namespaces into a string
	var namespaces []string
	for _, ns := range namespaceList.Items {
		namespaces = append(namespaces, ns.Name)
	}
	namespaceString := strings.Join(namespaces, ", ")

	fileList, err := getFileOptions()
	if err != nil {
		log.Fatalf("Error retrieving file options: %v", err)
	}

	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

	selects := createJobNameSelect(fileList, nil, cmdPrefix("select_first"))

	return executor.ExecuteOutput{
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: fmt.Sprintf("Please select the Job name. Available namespaces: %s", namespaceString),
			},
			Sections: []api.Section{
				{
					Selects: selects,
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

	initialOption := &api.OptionItem{
		Name:  state["first"],
		Value: state["first"],
	}
	selects := createJobNameSelect(fileList, initialOption, cmdPrefix("select_first"))

	sections := []api.Section{
		{
			Selects: selects,
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

		// Construct the flag key for the state
		flagKey := fmt.Sprintf("%s-%s", state["first"], option.Flags[0])

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
			if len(sections) < 2 {
				sections = append(sections, api.Section{})
			}
			sections[1].PlaintextInputs = append(sections[1].PlaintextInputs, api.LabelInput{
				Command: cmdPrefix(fmt.Sprintf("select_dynamic %s %s ", flagKey, option.Flags[0])),
				Text:        option.Description,
				Placeholder: "Please write parameter value",
				DispatchedAction: api.DispatchInputActionOnCharacter,
			})
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
				Plaintext: fmt.Sprintf("Please select the Job parameters for %s", state["first"] ),
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
	executor.Serve(map[string]go_plugin.Plugin{
		pluginName: &executor.Plugin{
			Executor: &MsgExecutor{
				state: make(map[string]map[string]string),
			},
		},
	})
}
