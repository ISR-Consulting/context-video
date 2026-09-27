// Package media cuts content into contiguous MediaSegment windows and replays
// them in media time for Live simulation and VOD precompute.
//
// Segment is a pure, deterministic function of a Source and a window size.
// Simulator replays segments instantly or paced by an injected Clock, so Live
// and VOD share the same MediaSegment contract and differ only in pacing.
//
// The package never opens, reads or decodes media bytes, never accesses the
// network and never calls a provider. Segments reference their source through
// sourceUri plus their window.
package media
