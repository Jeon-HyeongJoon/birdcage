package loopkit

// Go mirrors of loop/schema/*.schema.json. The schemas are the SSOT; these types only
// read/write what the schemas allow (validated before use).

type Policy struct {
	Version        int      `json:"version"`
	TerminalStates []string `json:"terminalStates"`
	Budgets        struct {
		MaxIterationsPerTask    int `json:"maxIterationsPerTask"`
		MaxIterationMinutes     int `json:"maxIterationMinutes"`
		StallAfterSameSignature int `json:"stallAfterSameSignature"`
	} `json:"budgets"`
	Evidence struct {
		SuccessRequires []Requirement `json:"successRequires"`
	} `json:"evidence"`
	HumanGates []struct {
		Action string `json:"action"`
	} `json:"humanGates"`
	Protected struct {
		Gate  string   `json:"gate"`
		Paths []string `json:"paths"`
	} `json:"protected"`
	Storage struct {
		Commit                []string `json:"commit"`
		Local                 []string `json:"local"`
		MaxCommittedFileBytes int64    `json:"maxCommittedFileBytes"`
		RedactPatterns        []string `json:"redactPatterns"`
		LocalRetentionDays    int      `json:"localRetentionDays"`
	} `json:"storage"`
	Sessions struct {
		OneTaskPerSession bool     `json:"oneTaskPerSession"`
		FreshContextFor   []string `json:"freshContextFor"`
		StartupRoutine    []string `json:"startupRoutine"`
	} `json:"sessions"`
}

// Requirement is "evidence of kind K at level L or stronger" (L1 strongest).
type Requirement struct {
	Kind     string `json:"kind"`
	MaxLevel string `json:"maxLevel"`
	What     string `json:"what,omitempty"`
}

type Backlog struct {
	Schema string `json:"$schema,omitempty"`
	Tasks  []Task `json:"tasks"`
}

type Task struct {
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	Mode       string        `json:"mode"`
	Status     string        `json:"status"`
	Passes     bool          `json:"passes"`
	Priority   int           `json:"priority,omitempty"`
	DependsOn  []string      `json:"dependsOn,omitempty"`
	Acceptance []Requirement `json:"acceptance"`
	Evidence   []string      `json:"evidence,omitempty"`
	BlockedBy  string        `json:"blockedBy,omitempty"`
}

type Entry struct {
	ID         string         `json:"id"`
	Iteration  string         `json:"iteration"`
	At         string         `json:"at"`
	Kind       string         `json:"kind"`
	Level      string         `json:"level"`
	Claim      string         `json:"claim"`
	Verdict    string         `json:"verdict"`
	Ref        Ref            `json:"ref"`
	Artifact   *Artifact      `json:"artifact,omitempty"`
	Values     map[string]any `json:"values,omitempty"`
	DurationMs float64        `json:"durationMs,omitempty"`
	Reproduced int            `json:"reproduced,omitempty"`
	Source     string         `json:"source,omitempty"`
	ObservedAt string         `json:"observedAt,omitempty"`
	Cites      []string       `json:"cites,omitempty"`
}

type Ref struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type Iteration struct {
	Schema            string     `json:"$schema,omitempty"`
	ID                string     `json:"id"`
	Task              string     `json:"task"`
	Session           Session    `json:"session"`
	StartedAt         string     `json:"startedAt"`
	EndedAt           string     `json:"endedAt,omitempty"`
	Base              GitRef     `json:"base"`
	Head              *GitRef    `json:"head,omitempty"`
	State             string     `json:"state"`
	Hypothesis        string     `json:"hypothesis"`
	Summary           string     `json:"summary,omitempty"`
	FailureSignatures []string   `json:"failureSignatures,omitempty"`
	Metrics           *Metrics   `json:"metrics,omitempty"`
	Retro             *Retro     `json:"retro,omitempty"`
	Next              []string   `json:"next,omitempty"`
	Approvals         []Approval `json:"approvals,omitempty"`
}

type Session struct {
	Kind       string `json:"kind"`
	ID         string `json:"id,omitempty"`
	Model      string `json:"model,omitempty"`
	Transcript string `json:"transcript,omitempty"`
}

type GitRef struct {
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty,omitempty"`
}

type Metrics struct {
	DurationMin     float64        `json:"durationMin"`
	Evidence        int            `json:"evidence"`
	EvidenceByLevel map[string]int `json:"evidenceByLevel"`
	Pass            int            `json:"pass"`
	Fail            int            `json:"fail"`
	Skip            int            `json:"skip"`
	Tokens          int            `json:"tokens,omitempty"`
	USD             float64        `json:"usd,omitempty"`
}

type Retro struct {
	NewChecks   []string `json:"newChecks,omitempty"`
	SpecChanges []string `json:"specChanges,omitempty"`
	Lessons     []string `json:"lessons,omitempty"`
	None        bool     `json:"none,omitempty"`
}

// Empty reports whether the retro produced nothing and did not declare "none".
func (r *Retro) Empty() bool {
	return r == nil || (!r.None && len(r.NewChecks) == 0 && len(r.SpecChanges) == 0 && len(r.Lessons) == 0)
}

type Approval struct {
	Action string `json:"action"`
	By     string `json:"by"`
	At     string `json:"at"`
	Note   string `json:"note,omitempty"`
}

// LevelRank maps L1..L5 to 1..5 (lower = stronger evidence).
func LevelRank(l string) int {
	if len(l) == 2 && l[0] == 'L' && l[1] >= '1' && l[1] <= '5' {
		return int(l[1] - '0')
	}
	return 99
}

// Satisfies reports whether passing evidence meets the requirement.
func Satisfies(e Entry, r Requirement) bool {
	return e.Verdict == "pass" && e.Kind == r.Kind && LevelRank(e.Level) <= LevelRank(r.MaxLevel)
}
