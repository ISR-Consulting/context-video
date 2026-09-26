package contracts

// TimeWindow is a temporal interval in media time, in milliseconds.
type TimeWindow struct {
	StartMs int64 `json:"startMs"`
	EndMs   int64 `json:"endMs"`
}
