package contracts

// ContentType identifies whether the asset is live or on demand.
type ContentType string

const (
	ContentTypeLive ContentType = "LIVE"
	ContentTypeVOD  ContentType = "VOD"
)

// ContentRef identifies the content being analyzed.
type ContentRef struct {
	ContentID   string      `json:"contentId"`
	ContentType ContentType `json:"contentType"`
}
