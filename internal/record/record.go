// Package record writes one JSON line per run.
//
// Without this you cannot tell whether the architecture works or merely costs
// more. Three derived numbers matter most: cost per *accepted* run, where
// candidates die, and — once a judge exists — how often it agrees with you.
package record

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Run struct {
	Run   int    `json:"run"`
	Task  string `json:"task"`
	Arm   string `json:"arm"`   // "race-2" | "solo-cheap" | "solo-frontier"
	Round int    `json:"round"` // capped at escalate.MaxRounds

	TestsSHA string `json:"tests_sha"`
	BaseSHA  string `json:"base_sha"`

	Candidates []Candidate `json:"candidates"`

	Winner string `json:"winner,omitempty"`
	Reason string `json:"reason,omitempty"`
	Rung   int    `json:"rung,omitempty"`

	ArchitectCostUSD float64 `json:"architect_cost_usd"`
	TotalCostUSD     float64 `json:"total_cost_usd"`

	// TotalWall is the *slowest* builder plus the serial phases — not the
	// sum of the builders, who ran concurrently. Conflating those is the
	// easiest way to make your own tool look worse than it is.
	TotalWall Duration `json:"total_wall"`

	At time.Time `json:"at"`
}

type Candidate struct {
	Builder string `json:"builder"`

	FailedAt     string   `json:"failed_at,omitempty"`
	FailingTests []string `json:"failing_tests,omitempty"`

	TouchedFrozen bool `json:"touched_frozen"`
	ViolatedArch  bool `json:"violated_arch"`

	DiffLines    int `json:"diff_lines"`
	FilesTouched int `json:"files_touched"`
	DepsAdded    int `json:"deps_added"`

	CostUSD float64  `json:"cost_usd"`
	Wall    Duration `json:"wall"`
}

// Duration marshals as a readable string rather than nanoseconds, because
// these files get read by people.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

// Append writes one run as a line of JSONL.
func Append(dir string, r Run) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "runs.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(r)
}
