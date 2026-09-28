// Package config loads and validates M03 experiment configuration files.
//
// The YAML files under configs/experiments are decoded strictly into a typed
// model: unknown fields, duplicate keys, trailing documents and type
// mismatches are rejected. Semantic validation then produces an immutable
// Config value. This package does not select or call providers. Provider
// values are labels recorded in the ExperimentResult configuration; the M05
// audio pipeline additionally uses audio.provider as the name of the STT
// adapter to run, and the M06 vision pipeline uses vision.provider as the name
// of the visual adapter and parses vision.sampling as its frame sampling
// policy ("uniform:N"). "TBD" names no adapter and no policy.
package config
