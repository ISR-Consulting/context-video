// Package harness is the M03 evaluation harness core.
//
// The harness owns ordered test-case iteration, failure policy, M01 output
// validation, canonical ExperimentResult construction and atomic result
// persistence. A Pipeline owns only content processing for one supplied test
// case. The harness selects no provider, replays no media and computes no
// quality metrics; unmeasured ExperimentResult metrics stay absent.
package harness
