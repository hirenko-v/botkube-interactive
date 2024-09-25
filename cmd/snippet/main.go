package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"strings"

	"github.com/hashicorp/go-plugin"
	"github.com/kubeshop/botkube/pkg/api"
	"github.com/kubeshop/botkube/pkg/api/executor"
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
	channelID = "C07MUPT2QRE"  // Replace with your channel ID
	message   = "test"     // Replace with your message
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



func (SnippetExecutor) Metadata(context.Context) (api.MetadataOutput, error) {
	return api.MetadataOutput{
		Version:     version,
		Description: description,
	}, nil
}

// Execute returns a given command as a response.
//
//nolint:gocritic  //hugeParam: in is heavy (80 bytes); consider passing it by pointer
func (SnippetExecutor) Execute(_ context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {

    // Load the YAML file
    data, err := ioutil.ReadFile("/config/comm_config.yaml")
    if err != nil {
        log.Fatalf("Error reading YAML file: %v", err)
    }

    // Parse the YAML
    var config Config
    err = yaml.Unmarshal(data, &config)
    if err != nil {
        log.Fatalf("Error parsing YAML file: %v", err)
    }

    // Get the botToken value
    botToken := config.Communications.DefaultGroup.SocketSlack.BotToken
    if botToken == "" {
        log.Fatalf("Error: botToken not found in YAML file")
    }

	command := "echo ok" // Replace with your command

	// Step 1: Execute the command
	content, err := exec.Command("bash", "-c", command).Output()
	if err != nil {
		log.Fatalf("Error executing command: %v", err)
	}

	contentStr := string(content)
	if contentStr == "" {
		contentStr = "null"
	}

	fileSize := len(contentStr)
	filename := fmt.Sprintf("YOUR_JOB_NAME-%d.log", 123456789) // Replace with job name and epoch

	// Step 2: Get the upload URL
	uploadURL, fileID, err := getUploadURL(botToken, filename, fileSize)
	if err != nil {
		log.Fatalf("Error getting upload URL: %v", err)
	}

	// Step 3: Upload the file
	err = uploadFile(uploadURL, contentStr)
	if err != nil {
		log.Fatalf("Error uploading file: %v", err)
	}

	// Step 4: Complete the upload and post the message
	err = completeUpload(botToken, fileID, channelID, message)
	if err != nil {
		log.Fatalf("Error completing upload: %v", err)
	}

	fmt.Printf("%s has been successfully executed\n", command)

	return executor.ExecuteOutput{
		Message: api.NewCodeBlockMessage("", false),
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

func main() {
	executor.Serve(map[string]plugin.Plugin{
		"snippet": &executor.Plugin{
			Executor: &SnippetExecutor{},
		},
	})
}
