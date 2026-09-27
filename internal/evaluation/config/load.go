package config

import (
	"bytes"
	"errors"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFile reads, strictly decodes and validates one experiment YAML file.
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, &Error{Stage: StageRead, Source: path, Err: err}
	}
	return Parse(path, data)
}

// Parse strictly decodes and validates one experiment YAML document.
// source labels errors and is typically the file path.
func Parse(source string, data []byte) (Config, error) {
	raw, err := decode(data)
	if err != nil {
		return Config{}, &Error{Stage: StageDecode, Source: source, Err: err}
	}
	return validate(source, raw)
}

func decode(data []byte) (file, error) {
	var raw file
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return raw, errors.New("empty document")
		}
		return raw, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return raw, err
		}
		return raw, errors.New("exactly one YAML document is allowed")
	}
	return raw, nil
}
