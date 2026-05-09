package transcriber

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Client communicates with the remote transcriber service.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a Client for the given host and port.
func New(host string, port int) *Client {
	return &Client{
		baseURL:    fmt.Sprintf("http://%s:%d", host, port),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// UploadResult is returned by Upload.
type UploadResult struct {
	JobID string
	Busy  bool // true when the server responded with 503
}

type uploadResponse struct {
	Status string `json:"status"`
	JobID  string `json:"job_id"`
}

// Upload uploads the audio file at filePath and returns the job ID.
// When the server is busy (503), Busy is true and JobID is empty.
func (c *Client) Upload(filePath string) (UploadResult, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return UploadResult{}, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return UploadResult{}, fmt.Errorf("creating form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return UploadResult{}, fmt.Errorf("writing form file: %w", err)
	}
	mw.Close()

	resp, err := c.httpClient.Post(c.baseURL+"/upload", mw.FormDataContentType(), &buf)
	if err != nil {
		return UploadResult{}, fmt.Errorf("uploading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return UploadResult{Busy: true}, nil
	}
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return UploadResult{}, fmt.Errorf("upload failed (%d): %s", resp.StatusCode, body)
	}

	var ur uploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&ur); err != nil {
		return UploadResult{}, fmt.Errorf("decoding upload response: %w", err)
	}
	return UploadResult{JobID: ur.JobID}, nil
}

// StatusResult is returned by Status.
type StatusResult struct {
	Done       bool
	Transcript string
}

type statusResponse struct {
	Status     string `json:"status"`
	Transcript string `json:"transcript"`
	Message    string `json:"message"`
}

// Status checks the status of a transcription job.
// Returns Done=false for still-running jobs, Done=true with Transcript when completed.
// Returns an error for server errors (500) or unknown job IDs (404).
func (c *Client) Status(jobID string) (StatusResult, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/status/" + jobID)
	if err != nil {
		return StatusResult{}, fmt.Errorf("checking status: %w", err)
	}
	defer resp.Body.Close()

	var sr statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return StatusResult{}, fmt.Errorf("decoding status response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return StatusResult{Done: true, Transcript: sr.Transcript}, nil
	case http.StatusAccepted:
		return StatusResult{Done: false}, nil
	case http.StatusInternalServerError:
		return StatusResult{}, fmt.Errorf("transcription failed: %s", sr.Message)
	case http.StatusNotFound:
		return StatusResult{}, fmt.Errorf("unknown job ID %q", jobID)
	default:
		return StatusResult{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
}
