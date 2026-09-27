// Package config loads and validates M03 experiment configuration files.
//
// The YAML files under configs/experiments are decoded strictly into a typed
// model: unknown fields, duplicate keys, trailing documents and type
// mismatches are rejected. Semantic validation then produces an immutable
// Config value. Provider values such as "TBD" are descriptive labels; this
// package does not select or call providers.
package config
