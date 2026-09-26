package contracts

// MediaSegment is one ingested slice of content.
type MediaSegment struct {
	SegmentID string         `json:"segmentId"`
	Content   ContentRef     `json:"content"`
	Window    TimeWindow     `json:"window"`
	SourceURI *string        `json:"sourceUri,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}
