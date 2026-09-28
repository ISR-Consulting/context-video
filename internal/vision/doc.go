// Package vision is the vendor-neutral visual perception port of the Context
// Intelligence pipeline.
//
// A [Sampler] chooses deterministic frame timestamps inside each media
// window. An [Analyzer] turns those frames into provider-neutral [Frame]
// detections, and [NewObservation] maps them onto the
// contracts.VisualObservation evidence contract. Adapters for concrete
// providers live in subpackages and are selected by name through a
// [Registry], so callers never import or name a vendor. Provider-native
// payloads never cross this package boundary.
//
// Detection confidence is carried exactly as the provider states it. A
// provider that supplies none cannot produce a valid VisualObservation, since
// the contract requires item-level confidence; nothing is defaulted. Whether a
// provider's confidence is calibrated is adapter-specific and documented by
// each adapter.
//
// Visual observations are evidence, not context: this package creates no
// ContextEvents and performs no fusion. Scene-change selection is not
// implemented; sampling is uniform within each window.
package vision
