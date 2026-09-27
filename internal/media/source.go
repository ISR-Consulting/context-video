package media

import "github.com/ISR-Consulting/context-video/pkg/contracts"

// Source describes one content item to segment. Callers copy these fields from
// their own model (for example a golden dataset test case), so this package
// does not depend on the evaluation packages.
type Source struct {
	Content contracts.ContentRef
	// URI is copied unchanged into every segment's sourceUri. It is never opened.
	URI string
	// DurationMs is the declared media duration in milliseconds.
	DurationMs int64
}
