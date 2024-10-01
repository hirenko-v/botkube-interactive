package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	go_plugin "github.com/hashicorp/go-plugin"
	"github.com/kubeshop/botkube/pkg/api"
	"github.com/kubeshop/botkube/pkg/api/executor"
	"github.com/kubeshop/botkube/pkg/plugin"
	"gopkg.in/yaml.v2"
)

type Config struct {
	CommunicationGroup string `yaml:"communicationGroup,omitempty"`
	Communications map[string]struct {
		SocketSlack struct {
			AppToken string `yaml:"appToken"`
			BotToken string `yaml:"botToken"`
			Enabled  bool   `yaml:"enabled"`
			Channels  map[string]struct {
				Name     string   `yaml:"name"`
				ID       string   `yaml:"id"`
				Bindings struct {
					Executors []string `yaml:"executors"`
					Sources   []string `yaml:"sources"`
				} `yaml:"bindings"`
			} `yaml:"channels"`
		} `yaml:"socketSlack"`
	} `yaml:"communications"`
}

const (
	configPath = "/config/comm_config.yaml"
)

type UploadURLResponse struct {
	UploadURL string `json:"upload_url"`
	FileID    string `json:"file_id"`
}

type CompleteUploadPayload struct {
	Files         []FileInfo `json:"files"`
	ChannelID     string     `json:"channel_id"`
	InitialComment string    `json:"initial_comment"`
}

type FileInfo struct {
	ID string `json:"id"`
}

const description = "snippet"

// version is set via ldflags by GoReleaser.
var version = "dev"
var configJSONSchema      string

// SnippetExecutor implements the Botkube executor plugin interface.
type SnippetExecutor struct{}

func getUploadURL(token, filename string, fileSize int) (string, string, error) {
	url := "https://slack.com/api/files.getUploadURLExternal"
	data := map[string]string{
		"filename": filename,
		"token":    token,
		"length":   fmt.Sprintf("%d", fileSize),
	}

	resp, err := postForm(url, data)
	if err != nil {
		return "", "", err
	}

	var result UploadURLResponse
	err = json.Unmarshal(resp, &result)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse response: %v", err)
	}

	if result.UploadURL == "null" {
		return "", "", fmt.Errorf("error getting upload URL: %s", string(resp))
	}

	return result.UploadURL, result.FileID, nil
}

func uploadFile(uploadURL, content string) error {
	req, err := http.NewRequest("POST", uploadURL, strings.NewReader(content))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to upload file: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error uploading file: %s", string(body))
	}

	return nil
}

func completeUpload(token, fileID, channelID, message string) error {
	url := "https://slack.com/api/files.completeUploadExternal"
	payload := CompleteUploadPayload{
		Files:         []FileInfo{{ID: fileID}},
		ChannelID:     channelID,
		InitialComment: message,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json;charset=utf-8")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error completing upload: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("error completing upload: %s", string(body))
	}

	return nil
}

func postForm(urlString string, data map[string]string) ([]byte, error) {
    form := url.Values{}
    for key, value := range data {
        form.Add(key, value)
    }

    resp, err := http.PostForm(urlString, form)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    return io.ReadAll(resp.Body)
}


const (
	kubectlVersion   = "v1.28.1"
)

func (SnippetExecutor) Metadata(context.Context) (api.MetadataOutput, error) {
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
		Description:      description,
		JSONSchema: api.JSONSchema{
			Value: configJSONSchema,
		},
	}, nil
}


// Execute returns a given command as a response.
//
//nolint:gocritic  //hugeParam: in is heavy (80 bytes); consider passing it by pointer
func (SnippetExecutor) Execute(ctx context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {
	var cmd, msg, message string

	msg, cmd, err := parseCmdAndMsg(in.Command)
	if err != nil {
		return executor.ExecuteOutput{}, err
	}

	var cfg Config
	err = plugin.MergeExecutorConfigs(in.Configs, &cfg)
	if err != nil {
		return executor.ExecuteOutput{}, err
	}
	
	botToken, channelID, err := getConfig(cfg.CommunicationGroup)

	// Step 1: Execute the command
	content, err := executeCommand(ctx, cmd, in.Context.KubeConfig)
	if err != nil {
		return executor.ExecuteOutput{}, err
	}
	if content == "" {
		content = "empty output"
	}
	fileSize := len(content)
	filename := fmt.Sprintf("%s.log", strconv.FormatInt(time.Now().Unix(), 10))

	// Step 2: Get the upload URL
	uploadURL, fileID, err := getUploadURL(botToken, filename, fileSize)
	if err != nil {
		return executor.ExecuteOutput{}, err
	}

	// Step 3: Upload the file
	err = uploadFile(uploadURL, content)
	if err != nil {
		return executor.ExecuteOutput{}, err
	}

	// fmt.Printf("%s has been successfully executed\n", command) 
	if msg != "" {
		message = fmt.Sprintf("%s please check attachement with the following name: %s", msg, filename)
	} else {
		message = fmt.Sprintf("Command %s result sent, please check attachement with the following name: %s", cmd, filename)
	}

		// Step 4: Complete the upload and post the message
		err = completeUpload(botToken, fileID, channelID, message)
		if err != nil {
			return executor.ExecuteOutput{}, err
		}

	return executor.ExecuteOutput{
		Message: api.NewCodeBlockMessage(fmt.Sprintf("Command %s result sent, please check attachement with the following name: %s", cmd, filename), false),
	}, nil
}

func (SnippetExecutor) Help(context.Context) (api.Message, error) {
	btnBuilder := api.NewMessageButtonBuilder()
	return api.Message{
		Sections: []api.Section{
			{
				Base: api.Base{
					Header:      "Run command and recieve resuilt ase snippet",
					Description: description,
				},
				Buttons: []api.Button{
					btnBuilder.ForCommandWithDescCmd("Run", "snippet -c echo 'hello world'"),
				},
			},
		},
	}, nil
}

func getConfig(group string) (string, string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "","", fmt.Errorf("error reading YAML file: %v", err)
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return "","", fmt.Errorf("error parsing YAML file: %v", err)
	}
	socketSlack, exists := config.Communications[group]
	if !exists {
		log.Fatalf("group %s not found", group)
	}
	
	botToken := socketSlack.SocketSlack.BotToken
	channelID := socketSlack.SocketSlack.Channels["default"].ID
	return botToken, channelID, nil
}

func parseCommand(cmd string) (action, value string) {
	parts := strings.Fields(cmd)
	if len(parts) > 1 {
		action = parts[0]
		value = strings.Join(parts[1:], " ")
	}
	return
}

func parseCmdAndMsg(command string) (string, string, error) {
	_, value := parseCommand(command)
	var cmd, msg string
	re := regexp.MustCompile(`(-\w)\s+['"]([^'"]*)['"]|(-\w)\s+(\S+)`)
	cre := regexp.MustCompile(`-c (.+)`)
	mFlagFound := false

	// Find all matches in the input string
	matches := re.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return "", "", fmt.Errorf("no valid flag-value pairs found in command: %s", command)
	}

	// Extract -c flag command
	cFlagMatches := cre.FindStringSubmatch(value)
	if len(cFlagMatches) < 2 {
		return "", "", fmt.Errorf("missing '-c' flag in command: %s", command)
	}
	cFlagAll := cFlagMatches[1]

	// Iterate over the matches and assign flag values
	for _, match := range matches {
		if mFlagFound {
			cmd = strings.Trim(cFlagAll, `"'`)
			continue
		}

		if match[1] == "-c" {
			cmd = match[2] // Capture quoted value (single or double quotes)
		} else if match[3] == "-c" {
			cmd = match[4] // Capture unquoted value
		}

		if match[1] == "-m" {
			msg = match[2]
			mFlagFound = true
		} else if match[3] == "-m" {
			msg = match[4]
			mFlagFound = true
		}
	}

	if cmd == "" {
		return "", "", fmt.Errorf("command not found in '-c' flag")
	}

	return msg, cmd, nil
}

func executeCommand(ctx context.Context, cmd string, kubeConfig []byte) (string, error) {
	if strings.HasPrefix(cmd, "kubectl") {
		kubeConfigPath, deleteFn, err := plugin.PersistKubeConfig(ctx, kubeConfig)
		if err != nil {
			return "", fmt.Errorf("error writing kubeconfig file: %v", err)
		}
		defer func() {
			if deleteErr := deleteFn(ctx); deleteErr != nil {
				fmt.Fprintf(os.Stderr, "failed to delete kubeconfig file %s: %v", kubeConfigPath, deleteErr)
			}
		}()
		envs := map[string]string{
			"KUBECONFIG": kubeConfigPath,
		}

		out, err := plugin.ExecuteCommand(ctx, cmd, plugin.ExecuteCommandEnvs(envs))
		return out.Stdout, err
	}

	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		return "", fmt.Errorf("failed to run command %s: %v", cmd, err)
	}
	return string(out), nil
}

func main() {
	executor.Serve(map[string]go_plugin.Plugin{
		"snippet": &executor.Plugin{
			Executor: &SnippetExecutor{},
		},
	})
}
