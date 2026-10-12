// Package openai is a context Reasoner adapter for OpenAI Chat Completions
// (cheap default: gpt-4o-mini).
//
// For each EvidenceGroup it sends the versioned context-reasoning-v2 system
// prompt and evidence document with a per-group JSON schema response format.
// The answer is parsed strictly onto provider-neutral Candidates; native
// OpenAI payloads never leave this package. Credentials come from options or
// OPENAI_API_KEY. A group without evidence yields no candidates.
package openai
