// Package quality scores ContextEvent outputs against golden ground truth.
//
// M09 match policy (v1, lexical):
//   - Annotation windows and event windows match when their media-time
//     intervals overlap: startA < endB && startB < endA.
//   - Labels compare by exact string equality after the annotation guidelines
//     (entity type+value; topic/object/brand value). No synonym, stem, case
//     fold or language-normalization step is applied.
//   - required: counted for recall (hit if any overlapping event emits it).
//   - optional: neither rewarded nor penalized.
//   - forbidden: counted as a forbidden hit when emitted in an overlapping event.
//
// Synonym / near-match policy is deliberately deferred; see
// docs/poc/M09-EVALUATION-REPORT.md. This package does not change any schema.
package quality
