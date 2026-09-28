package whispercpp

import (
	"encoding/json"
	"errors"
	"strings"
)

// output is the subset of the whisper-cli -oj document the adapter reads.
// Other fields are ignored so minor format changes stay compatible.
type output struct {
	Result *struct {
		Language string `json:"language"`
	} `json:"result"`
	Transcription *[]struct {
		Text string `json:"text"`
	} `json:"transcription"`
}

type parsed struct {
	text     string
	language string
}

func parseOutput(data []byte) (parsed, error) {
	var out output
	if err := json.Unmarshal(data, &out); err != nil {
		return parsed{}, err
	}
	if out.Transcription == nil {
		return parsed{}, errors.New(`missing "transcription" array`)
	}
	parts := make([]string, 0, len(*out.Transcription))
	for _, segment := range *out.Transcription {
		if text := strings.TrimSpace(segment.Text); text != "" {
			parts = append(parts, text)
		}
	}
	p := parsed{text: strings.Join(parts, " ")}
	if out.Result != nil {
		p.language = strings.TrimSpace(out.Result.Language)
	}
	return p, nil
}
