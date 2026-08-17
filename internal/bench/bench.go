// Package bench runs the same tasks through different architectures and prints
// the comparison.
//
// This is not a report at the end of a run — it is a separate mode, and it is
// the part people will actually judge the project on. Without it you have an
// opinion; with it you have a table.
package bench

import "fmt"

// Arm is one architecture under test.
type Arm string

const (
	// Race2 is the thing being built: two cheap builders, tests select.
	Race2 Arm = "race-2"
	// SoloCheap answers "is racing worth it?" — if one cheap builder gets
	// close to the same success rate, the second builder is waste.
	SoloCheap Arm = "solo-cheap"
	// SoloFrontier answers "is the whole architecture worth it?" — one
	// expensive model, single pass, no orchestration. If it wins on both
	// success and cost, ship it and keep the tests.
	SoloFrontier Arm = "solo-frontier"
)

// Task is one benchmark case.
type Task struct {
	Name   string
	Prompt string
	// Tests are written ONCE per task and reused by every arm. If each arm
	// regenerates them, each sits a different exam and the comparison means
	// nothing. This is the fairness rule and it is not optional.
	TestsDir string
	BaseSHA  string
}

// Summary is one row of the output table.
type Summary struct {
	Arm         Arm
	SuccessRate float64
	AvgCostUSD  float64
	AvgWallSec  float64
	Candidates  int
}

// Run executes every task through every arm, `repeats` times each. Models are
// non-deterministic, so a single run per cell tells you nothing — five is the
// usual minimum.
//
// TODO: implement. Grading needs no LLM judge: the frozen suite decides
// pass/fail, which is what makes the resulting numbers arguable-with by nobody.
func Run(tasks []Task, arms []Arm, repeats int) ([]Summary, error) {
	return nil, fmt.Errorf("bench.Run: not implemented")
}
