package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxBody bounds response bodies kept for errors and successful JSON decode.
const maxBody = 1 << 20

type transcriptionResponse struct {
	Text     string   `json:"text"`
	Language string   `json:"language"`
	Duration *float64 `json:"duration"`
}

func (a *Adapter) transcribeFile(ctx context.Context, wav string) (transcriptionResponse, error) {
	f, err := os.Open(wav)
	if err != nil {
		return transcriptionResponse{}, err
	}
	defer f.Close()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filepath.Base(wav))
	if err != nil {
		return transcriptionResponse{}, err
	}
	if _, err := io.Copy(part, f); err != nil {
		return transcriptionResponse{}, err
	}
	if err := w.WriteField("model", a.cfg.Model); err != nil {
		return transcriptionResponse{}, err
	}
	if err := w.WriteField("response_format", "json"); err != nil {
		return transcriptionResponse{}, err
	}
	if a.cfg.Language != "" && a.cfg.Language != "auto" {
		if err := w.WriteField("language", a.cfg.Language); err != nil {
			return transcriptionResponse{}, err
		}
	}
	if err := w.Close(); err != nil {
		return transcriptionResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.BaseURL+"/audio/transcriptions", &body)
	if err != nil {
		return transcriptionResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := a.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return transcriptionResponse{}, ctxErr
		}
		return transcriptionResponse{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return transcriptionResponse{}, err
	}
	if len(data) > maxBody {
		return transcriptionResponse{}, fmt.Errorf("response body exceeds %d bytes", maxBody)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return transcriptionResponse{}, fmt.Errorf("openai transcriptions HTTP %d: %s", resp.StatusCode, excerpt(data))
	}
	var out transcriptionResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return transcriptionResponse{}, fmt.Errorf("decode transcription response: %w", err)
	}
	return out, nil
}

func excerpt(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 512 {
		s = s[:512] + "…"
	}
	return s
}
