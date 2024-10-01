package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	go_plugin "github.com/hashicorp/go-plugin"
	"github.com/kubeshop/botkube/pkg/api"
	"github.com/kubeshop/botkube/pkg/api/executor"
	"github.com/kubeshop/botkube/pkg/plugin"
	"github.com/slack-go/slack"
)

const (
	description = "Run Job."
	pluginName  = "job"
	kubectlVersion   = "v1.28.1"

)

// version is set via ldflags by GoReleaser.
var version = "dev"

// MsgExecutor implements the Botkube executor plugin interface.
type MsgExecutor struct {
}

// JSON structure for the script output
type BotKubeAnnotation struct {
	Flag        string   `json:"flag"`
	Description string   `json:"description"`
	Values      []string `json:"values,omitempty"`
	Default     string   `json:"default,omitempty"`
	Type        string   `json:"type"`
}

type CronJobs struct {
    Metadata struct {
        Annotations map[string]string `json:"Annotations"`
		Name string `json:"name"`
		Namespace string `json:"namespace"`
    } `json:"metadata"`
}

type CronJobsList struct {
    Items []CronJobs `json:"items"`
}
type Arg struct {
	Flag        string `json:"flag"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Values      []string `json:"values,omitempty"`
}

type Job struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Args      []Arg  `json:"args"`
}

// Metadata returns details about the Msg plugin.
func (MsgExecutor) Metadata(context.Context) (api.MetadataOutput, error) {
	return api.MetadataOutput{
		Dependencies: map[string]api.Dependency{
			"kubectl": {
				URLs: map[string]string{
					"windows/amd64": fmt.Sprintf("https://dl.k8s.io/release/%s/bin/windows/amd64/kubectl.exe", kubectlVersion),
					"darwin/amd64":  fmt.Sprintf("https://dl.k8s.io/release/%s/bin/darwin/amd64/kubectl", kubectlVersion),
					"darwin/arm64":  fmt.Sprintf("https://dl.k8s.io/release/%s/bin/darwin/arm64/kubectl", kubectlVersion),
					"linux/amd64":   fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/amd64/kubectl", kubectlVersion),
					"linux/s390x":   fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/s390x/kubectl", kubectlVersion),
					"linux/ppc64le": fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/ppc64le/kubectl", kubectlVersion),
					"linux/arm64":   fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/arm64/kubectl", kubectlVersion),
					"linux/386":     fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/386/kubectl", kubectlVersion),
				},
			},
		},
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
	envs := map[string]string{
		"KUBECONFIG": kubeConfigPath,
	}
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	}
	slackState := in.Context.SlackState
	details := e.extractStateDetails(slackState)

	// Parse the action and value from the command
	action, value := parseCommand(in.Command)

	switch action {
	case "select_first":
		return showBothSelects(ctx, envs, details), nil

	case "select_dynamic":
		return showBothSelects(ctx, envs, details), nil

	case "run":
		fields := strings.Fields(value)
		args := fields[2:]
		jobName := fmt.Sprintf("%s-%s",fields[0], strconv.FormatInt(time.Now().Unix(), 10))
		filePath := fmt.Sprintf("/tmp/%s-%s.json",jobName, uuid.New())
		runCmd := fmt.Sprintf("kubectl create job --from=cronjob/%s -n %s %s --dry-run -ojson", fields[0], fields[1], jobName)
		out, _ := plugin.ExecuteCommand(ctx, runCmd, plugin.ExecuteCommandEnvs(envs))
		var cronJob map[string]interface{}
		err := json.Unmarshal([]byte(out.Stdout), &cronJob)
		if err != nil {
			fmt.Println("Error unmarshalling JSON:", err)
		}
		annotations := cronJob["metadata"].(map[string]interface{})["annotations"].(map[string]interface{})
		annotations["botkube"] = "true" 
		// Navigate to the container args
		template := cronJob["spec"].(map[string]interface{})["template"].(map[string]interface{})
		container := template["spec"].(map[string]interface{})["containers"].([]interface{})[0].(map[string]interface{})

		// Modify the first container args
		container["args"] = args

		// Marshal the modified map back to JSON
		modifiedJSON, err := json.MarshalIndent(cronJob, "", "  ")
		if err != nil {
			fmt.Println("Error marshalling JSON:", err)
		}

		// // Save the patched JSON to a file
		err = os.WriteFile(filePath, modifiedJSON, 0644) // Create or overwrite the file
		if err != nil {
			log.Fatalf("error writing patched JSON to file: %w", err)
		}
		createCmd := fmt.Sprintf("kubectl apply -f %s", filePath)
		plugin.ExecuteCommand(ctx, createCmd, plugin.ExecuteCommandEnvs(envs))
		defer os.RemoveAll(filePath)
		return executor.ExecuteOutput{
			Message: api.NewCodeBlockMessage(fmt.Sprintf("Job %s is started",jobName), true),
		}, nil
	}

	if strings.TrimSpace(in.Command) == pluginName {
		return initialMessages(ctx, envs, e), nil
	}

	msg := fmt.Sprintf("Plain command: %s", in.Command)
	return executor.ExecuteOutput{
		Message: api.NewCodeBlockMessage(msg, true),
	}, nil


}

type stateDetails struct {
	job         string
	params      map[string]string
}

func (e *MsgExecutor) extractStateDetails(state *slack.BlockActionStates) stateDetails {
	if state == nil {
		return stateDetails{}
	}

	details := stateDetails{
		params: make(map[string]string),
	}
	for _, blocks := range state.Values {

		for id, act := range blocks {
			id_full := strings.TrimPrefix(id, pluginName)
			id_cmd := strings.Fields(id_full)[0]
			switch id_cmd {
			case "select_first":
				details.job = act.SelectedOption.Value
			case "select_dynamic":
				key := strings.Fields(id_full)[1]
				if act.Value != "" {
					details.params[key] = act.Value
				} else if act.SelectedOption.Value != "" {
					details.params[key] = act.SelectedOption.Value
				}


			}
			
		}
	}
	return details
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


func getBotkubeJobs(ctx context.Context, envs map[string]string) ([]Job) {
	var jobList []Job

	runCmd := "kubectl get cronjobs -A -ojson"
	out, _ := plugin.ExecuteCommand(ctx, runCmd, plugin.ExecuteCommandEnvs(envs))
	var cronJobsResList CronJobsList
	json.Unmarshal([]byte(out.Stdout), &cronJobsResList)
	for _,cronJob := range cronJobsResList.Items {
		_, ok := cronJob.Metadata.Annotations["botkubeJobArgs"]
		if ok {
			var args []Arg
			json.Unmarshal([]byte(cronJob.Metadata.Annotations["botkubeJobArgs"]), &args)
			jobList = append(jobList, Job{
				Name: cronJob.Metadata.Name,
				Namespace: cronJob.Metadata.Namespace,
				Args: args,
			})
		}
	}
	return jobList
}

func initialMessages(ctx context.Context, envs map[string]string, e *MsgExecutor) executor.ExecuteOutput {
	var jobList []api.OptionItem
	jobs := getBotkubeJobs(ctx, envs)
	for _, job := range jobs {
		jobList = append(jobList, api.OptionItem{
			Name:  job.Name,
			Value: job.Name,
		})
	}

	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

	selects := createJobNameSelect(jobList, nil, cmdPrefix("select_first"))

	return executor.ExecuteOutput{
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: "Please select the Job name",
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
func showBothSelects(ctx context.Context, envs map[string]string, details stateDetails) executor.ExecuteOutput {
	var jobList []api.OptionItem
	jobs := getBotkubeJobs(ctx, envs)
	for _, job := range jobs {
		jobList = append(jobList, api.OptionItem{
			Name:  job.Name,
			Value: job.Name,
		})
	}

	btnBuilder := api.NewMessageButtonBuilder()
	cmdPrefix := func(cmd string) string {
		return fmt.Sprintf("%s %s %s", api.MessageBotNamePlaceholder, pluginName, cmd)
	}

	initialOption := &api.OptionItem{
		Name:  details.job,
		Value: details.job,
	}
	selects := createJobNameSelect(jobList, initialOption, cmdPrefix("select_first"))

	sections := []api.Section{
		{
			Selects: selects,
		},
	}
	var namespace string
	var jobArgs []Arg
	// Create multiple dropdowns based on the options in the script output
	for _, job := range jobs {
		if job.Name == details.job {
			namespace = job.Namespace
			jobArgs = job.Args
			for _, option := range job.Args {
				// Construct the flag key for the state
				flagKey := fmt.Sprintf("%s-%s", details.job, option.Flag)

				if option.Type == "bool" || option.Type == "dropdown" {

					var dropdownOptions []api.OptionItem
					boolValues := []string{"true", "false"}
					values := option.Values; if option.Type == "bool" { values = boolValues }
					for _, value := range values {
						dropdownOptions = append(dropdownOptions, api.OptionItem{
							Name:  value,
							Value: fmt.Sprintf("%s %s", option.Flag, value),
						})
					}


					// Check if there's an InitialOption and update the state if it’s not already set
					if _, exists := details.params[flagKey]; !exists && option.Default != "" {
						details.params[flagKey] = fmt.Sprintf("%s %s", option.Flag, option.Default)
					}

					var initialOption *api.OptionItem
					if option.Default != "" {
						initialOption = &api.OptionItem{
							Name:  option.Default,
							Value: fmt.Sprintf("%s %s", option.Flag, option.Default),
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
						Command: cmdPrefix(fmt.Sprintf("select_dynamic %s %s ", flagKey, option.Flag)),
						Text:        option.Description,
						Placeholder: "Please write parameter value",
						DispatchedAction: api.DispatchInputActionOnCharacter,
					})
				}
			}
		}
	}

	// If all selections are made, show the run button
	if allSelectionsMade(details, jobArgs) {
		code := buildFinalCommand(jobArgs, namespace, details)
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
				Plaintext: fmt.Sprintf("Please select the Job parameters for %s", details.job),
			},
			Sections:          sections,
			OnlyVisibleForYou: true,
			ReplaceOriginal:   true,
		},
	}
}

// Helper function to check if all selections are made
func allSelectionsMade(details stateDetails, options []Arg) bool {
	for _, option := range options {
		if details.params[fmt.Sprintf("%s-%s", details.job, option.Flag)] == "" {
			return false
		}
	}
	return true
}

// Helper function to build the final command based on all selections
func buildFinalCommand(options []Arg, namespace string, details stateDetails) string {
	var commandParts []string

	// Add the first selection (e.g., job name)
	commandParts = append(commandParts, details.job)
	commandParts = append(commandParts, namespace)

	// Add options in the same order as they appear in the script output
	for _, option := range options {
		// Construct the key as used in the state map
		flagKey := fmt.Sprintf("%s-%s", details.job, option.Flag)
		part := fmt.Sprintf("%s %s",option.Flag, details.params[flagKey])
		if option.Type == "bool" {
			if strings.Fields(details.params[flagKey])[1] == "true" {
				part = strings.Fields(details.params[flagKey])[0]
			} else {
				continue
			}
		}
		commandParts = append(commandParts, part)
	}

	return fmt.Sprintf("job run %s", strings.Join(commandParts, " "))
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
			},
		},
	})
}
