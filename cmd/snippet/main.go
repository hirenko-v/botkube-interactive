package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Config defines the structure of the configuration YAML file.
type Config struct {
	Communications struct {
		DefaultGroup struct {
			SocketSlack struct {
				BotToken string `yaml:"botToken"`
			} `yaml:"socketSlack"`
		} `yaml:"default-group"`
	} `yaml:"communications"`
}

const (
	channelID      = "C07MUPT2QRE"
	kubectlVersion = "v1.28.1"
	description    = "snippet"
	version        = "dev"
)

type UploadURLResponse struct {
	UploadURL string `json:"upload_url"`
	FileID    string `json:"file_id"`
}

type CompleteUploadPayload struct {
	Files          []FileInfo `json:"files"`
	ChannelID      string     `json:"channel_id"`
	InitialComment string     `json:"initial_comment"`
}

type FileInfo struct {
	ID string `json:"id"`
}

// SnippetExecutor implements the Botkube executor plugin interface.
type SnippetExecutor struct{}

func getBotToken() (string, error) {
	data, err := os.ReadFile("/config/comm_config.yaml")
	if err != nil {
		return "", fmt.Errorf("error reading YAML file: %v", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("error parsing YAML file: %v", err)
	}

	botToken := config.Communications.DefaultGroup.SocketSlack.BotToken
	if botToken == "" {
		return "", errors.New("bot token not found in configuration")
	}
	return botToken, nil
}

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
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", "", fmt.Errorf("failed to parse response: %v", err)
	}

	if result.UploadURL == "" {
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
		Files:          []FileInfo{{ID: fileID}},
		ChannelID:      channelID,
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

func (SnippetExecutor) Metadata(context.Context) (api.MetadataOutput, error) {
	return api.MetadataOutput{
		Dependencies: map[string]api.Dependency{
			"kubectl": {
				URLs: map[string]string{
					"windows/amd64": fmt.Sprintf("https://dl.k8s.io/release/%s/bin/windows/amd64/kubectl.exe", kubectlVersion),
					"darwin/amd64":  fmt.Sprintf("https://dl.k8s.io/release/%s/bin/darwin/amd64/kubectl", kubectlVersion),
					"darwin/arm64":  fmt.Sprintf("https://dl.k8s.io/release/%s/bin/darwin/arm64/kubectl", kubectlVersion),
					"linux/amd64":   fmt.Sprintf("https://dl.k8s.io/release/%s/bin/linux/amd64/kubectl", kubectlVersion),
				},
			},
		},
		Version:     version,
		Description: description,
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
					btnBuilder.ForCommandWithDescCmd("Run", "snippet 'hello world'"),
				},
			},
		},
	}, nil
}


func (SnippetExecutor) Execute(ctx context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {
	_, value := parseCommand(in.Command)
	cmd, msg := parseFlags(value)

	if cmd == "" {
		return executor.ExecuteOutput{}, errors.New("command not provided")
	}

	// Load bot token from config
	botToken, err := getBotToken()
	if err != nil {
		return executor.ExecuteOutput{}, err
	}

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
	if err := uploadFile(uploadURL, content); err != nil {
		return executor.ExecuteOutput{}, err
	}

	// Step 4: Complete the upload and post the message
	message := createUploadMessage(cmd, filename, msg)
	if err := completeUpload(botToken, fileID, channelID, message); err != nil {
		return executor.ExecuteOutput{}, err
	}

	return executor.ExecuteOutput{
		Message: api.NewCodeBlockMessage(message, false),
	}, nil
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

func createUploadMessage(cmd, filename, msg string) string {
	return fmt.Sprintf("Executed command: %s \n%s: %s", cmd, filename, msg)
}

func parseCommand(input string) (string, string) {
	re := regexp.MustCompile(`(?m)^execute (.+)$`)
	match := re.FindStringSubmatch(input)
	if len(match) > 1 {
		return match[0], match[1]
	}
	return "", ""
}

func parseFlags(input string) (string, string) {
	// Example flag parsing logic
	if input == "" {
		return "", ""
	}
	return input, "Parsed flags"
}

func main() {
	executor.Serve(map[string]go_plugin.Plugin{
		"snippet": &executor.Plugin{
			Executor: &SnippetExecutor{},
		},
	})
}
