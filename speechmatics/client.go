package speechmatics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"time"
)

const (
	BaseURL = "https://asr.api.speechmatics.com/v2"
)

type Client struct {
	APIKey     string
	HTTPClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
	}
}

type JobConfig struct {
	Type                string             `json:"type"`
	TranscriptionConfig *TranscriptionConfig `json:"transcription_config,omitempty"`
	AlignmentConfig     *AlignmentConfig     `json:"alignment_config,omitempty"`
}

type TranscriptionConfig struct {
	Language                 string            `json:"language"`
	Domain                   string            `json:"domain,omitempty"`
	OutputLocale             string            `json:"output_locale,omitempty"`
	OperatingPoint           OperatingPoint    `json:"operating_point,omitempty"`
	AdditionalVocab          []AdditionalVocab `json:"additional_vocab,omitempty"`
	Diarization              string            `json:"diarization,omitempty"`
	SpeakerChangeSensitivity float64           `json:"speaker_change_sensitivity,omitempty"`
}

type OperatingPoint string

const (
	OperatingPointStandard OperatingPoint = "standard"
	OperatingPointEnhanced OperatingPoint = "enhanced"
)

type AdditionalVocab struct {
	Content string   `json:"content"`
	Sounds  []string `json:"sounds,omitempty"`
}

type PunctuationOverrides struct {
	// Add specific punctuation override fields as needed
}

type SpeakerDiarizationConfig struct {
	SpeakerSensitivity float64 `json:"speaker_sensitivity,omitempty"`
}

type AlignmentConfig struct {
	Language string `json:"language"`
}

type JobResponse struct {
	ID string `json:"id"`
}

type JobDetails struct {
	CreatedAt time.Time `json:"created_at"`
	DataName  string    `json:"data_name"`
	TextName  string    `json:"text_name,omitempty"`
	Duration  int       `json:"duration"`
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Config    JobConfig `json:"config"`
}

type AlignmentTag string

const (
	WordStartAndEnd AlignmentTag = "word_start_and_end"
	OnePerLine      AlignmentTag = "one_per_line"
)


func (c *Client) CreateJob(ctx context.Context, audioFilePath string, config JobConfig) (*JobResponse, error) {
	url := fmt.Sprintf("%s/jobs", BaseURL)

	// Prepare the multipart form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add the audio file
	file, err := os.Open(audioFilePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	part, err := writer.CreateFormFile("data_file", audioFilePath)
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return nil, err
	}

	// Add the config
	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	err = writer.WriteField("config", string(configJSON))
	if err != nil {
		return nil, err
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send the request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("unexpected status code: %d, failed to read response body: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("unexpected status code: %d, response body: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var jobResponse JobResponse
	err = json.NewDecoder(resp.Body).Decode(&jobResponse)
	if err != nil {
		return nil, err
	}

	return &jobResponse, nil
}

func (c *Client) GetJobDetails(ctx context.Context, jobID string) (*JobDetails, error) {
	url := fmt.Sprintf("%s/jobs/%s", BaseURL, jobID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var wrappedResponse struct {
		Job JobDetails `json:"job"`
	}
	err = json.NewDecoder(resp.Body).Decode(&wrappedResponse)
	if err != nil {
		return nil, err
	}

	return &wrappedResponse.Job, nil
}

func (c *Client) GetTranscript(ctx context.Context, jobID string, format string) (string, error) {
	var url string
	switch format {
	case "json":
		url = fmt.Sprintf("%s/jobs/%s/transcript?format=json-v2", BaseURL, jobID)
	case "txt":
		url = fmt.Sprintf("%s/jobs/%s/transcript?format=txt", BaseURL, jobID)
	case "srt":
		url = fmt.Sprintf("%s/jobs/%s/transcript?format=srt", BaseURL, jobID)
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	transcript, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(transcript), nil
}

func (c *Client) DeleteJob(ctx context.Context, jobID string) error {
	url := fmt.Sprintf("%s/jobs/%s", BaseURL, jobID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) CreateAlignmentJob(ctx context.Context, audioFilePath, textFilePath string, config JobConfig) (*JobResponse, error) {
	url := fmt.Sprintf("%s/jobs", BaseURL)

	// Prepare the multipart form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add the audio file
	audioFile, err := os.Open(audioFilePath)
	if err != nil {
		return nil, err
	}
	defer audioFile.Close()

	audioPart, err := writer.CreateFormFile("data_file", audioFilePath)
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(audioPart, audioFile)
	if err != nil {
		return nil, err
	}

	// Add the text file
	textFile, err := os.Open(textFilePath)
	if err != nil {
		return nil, err
	}
	defer textFile.Close()

	textPart, err := writer.CreateFormFile("text_file", textFilePath)
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(textPart, textFile)
	if err != nil {
		return nil, err
	}

	// Add the config
	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	err = writer.WriteField("config", string(configJSON))
	if err != nil {
		return nil, err
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send the request
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Parse the response
	var jobResponse JobResponse
	err = json.NewDecoder(resp.Body).Decode(&jobResponse)
	if err != nil {
		return nil, err
	}

	return &jobResponse, nil
}

func (c *Client) GetAlignment(ctx context.Context, jobID string, tags AlignmentTag) (string, error) {
	url := fmt.Sprintf("%s/jobs/%s/alignment?tags=%s", BaseURL, jobID, tags)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	alignment, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(alignment), nil
}

func (c *Client) SubmitAndWaitForAlignment(ctx context.Context, audioFilePath, textFilePath string, alignmentConfig AlignmentConfig, pollInterval time.Duration, tags AlignmentTag) (string, error) {
	config := JobConfig{
		Type:            "alignment",
		AlignmentConfig: &alignmentConfig,
	}
	jobResponse, err := c.CreateAlignmentJob(ctx, audioFilePath, textFilePath, config)
	if err != nil {
		return "", fmt.Errorf("failed to create alignment job: %w", err)
	}

	_, err = c.WaitForJobCompletion(ctx, jobResponse.ID, pollInterval)
	if err != nil {
		return "", fmt.Errorf("failed while waiting for alignment job completion: %w", err)
	}

	alignment, err := c.GetAlignment(ctx, jobResponse.ID, tags)
	if err != nil {
		return "", fmt.Errorf("failed to get alignment: %w", err)
	}

	return alignment, nil
}

func (c *Client) WaitForJobCompletion(ctx context.Context, jobID string, pollInterval time.Duration) (*JobDetails, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			jobDetails, err := c.GetJobDetails(ctx, jobID)
			if err != nil {
				return nil, err
			}

			switch jobDetails.Status {
			case "done":
				return jobDetails, nil
			case "rejected", "deleted", "expired":
				return nil, fmt.Errorf("job failed with status: %s", jobDetails.Status)
			}
		}
	}
}

func (c *Client) SubmitAndWaitForTranscript(ctx context.Context, audioFilePath string, transcriptionConfig TranscriptionConfig, pollInterval time.Duration) (string, error) {
	config := JobConfig{
		Type:                "transcription",
		TranscriptionConfig: &transcriptionConfig,
	}
	jobResponse, err := c.CreateJob(ctx, audioFilePath, config)
	if err != nil {
		return "", fmt.Errorf("failed to create job: %w", err)
	}

	_, err = c.WaitForJobCompletion(ctx, jobResponse.ID, pollInterval)
	if err != nil {
		return "", fmt.Errorf("failed while waiting for job completion: %w", err)
	}

	transcript, err := c.GetTranscript(ctx, jobResponse.ID, "txt") // Default to txt format
	if err != nil {
		return "", fmt.Errorf("failed to get transcript: %w", err)
	}

	return transcript, nil
}

func (c *Client) ListJobs(ctx context.Context) ([]JobDetails, error) {
	url := fmt.Sprintf("%s/jobs", BaseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var response struct {
		Jobs []JobDetails `json:"jobs"`
	}
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return nil, err
	}

	return response.Jobs, nil
}
