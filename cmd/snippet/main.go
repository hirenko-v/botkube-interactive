package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	go_plugin "github.com/hashicorp/go-plugin"
	"github.com/kubeshop/botkube/pkg/api"
	"github.com/kubeshop/botkube/pkg/api/executor"
	"github.com/kubeshop/botkube/pkg/plugin"
	"gopkg.in/yaml.v2"
)

// Define the structure of your YAML file
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
	channelID = "C07MUPT2QRE"
	message   = "Output:"
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
		Description: description,
	}, nil
}

// Execute returns a given command as a response.
//
//nolint:gocritic  //hugeParam: in is heavy (80 bytes); consider passing it by pointer
func (SnippetExecutor) Execute(ctx context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {

	_, value := parseCommand(in.Command)

    // Load the YAML file
    data, err := ioutil.ReadFile("/config/comm_config.yaml")
    if err != nil {
        log.Fatalf("Error reading YAML file: %v", err)
    }

    // Parse the YAML
    var config Config
    err = yaml.Unmarshal(data, &config)
    if err != nil {
		return executor.ExecuteOutput{}, err
    }

    // Get the botToken value
    botToken := config.Communications.DefaultGroup.SocketSlack.BotToken
    if botToken == "" {
		return executor.ExecuteOutput{}, errors.New("Bottoken not found")
    }

	// Step 1: Execute the command
	content := ""
	if strings.HasPrefix(value, "kubectl") {
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

		out, err := plugin.ExecuteCommand(ctx, value, plugin.ExecuteCommandEnvs(envs))
		content = out.Stdout
	} else {
		out, err := exec.Command("sh", "-c", value).Output()
		if err != nil {
			return executor.ExecuteOutput{}, errors.New(fmt.Sprintf("Failed to run command, %s", err))
		}
		content = string(out)
	}
	if content == "" { content = "empty output" }
	fileSize := len(content)
	filename := fmt.Sprintf("%s.log",  strconv.FormatInt(time.Now().Unix(), 10))

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

	// Step 4: Complete the upload and post the message
	err = completeUpload(botToken, fileID, channelID, message)
	if err != nil {
		return executor.ExecuteOutput{}, err
	}

	// fmt.Printf("%s has been successfully executed\n", command)

	return executor.ExecuteOutput{
		Message: api.NewCodeBlockMessage(fmt.Sprintf("Command %s result sent, please check attachement the following name: %s", value, filename), false),
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

func parseCommand(cmd string) (action, value string) {
	parts := strings.Fields(cmd)
	if len(parts) > 1 {
		action = parts[0]
		value = strings.Join(parts[1:], " ")
	}
	return
}

func main() {
	executor.Serve(map[string]go_plugin.Plugin{
		"snippet": &executor.Plugin{
			Executor: &SnippetExecutor{},
		},
	})
}
