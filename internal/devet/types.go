package devet

// ScenarioResult is the structured response from /api/scenario/build
type ScenarioResult struct {
	Status string    `json:"status"`
	Chain  ChainInfo `json:"chain"`
}

// ChainInfo holds metadata about a delegation chain.
type ChainInfo struct {
	RootAIDHash    string `json:"root_aid_hash"`
	TotalAgents    int    `json:"total_agents"`
	MaxDepth       int    `json:"max_depth"`
	GrantCount     int    `json:"grant_count"`
	RootChainSteps int    `json:"root_chain_steps"`
}

// VerificationResult from /api/chain/verify
type VerificationResult struct {
	Status string       `json:"status"`
	Result VerifyDetail `json:"result"`
}

// VerifyDetail is the core verification output.
type VerifyDetail struct {
	Authentic        bool           `json:"authentic"`
	BlamePath        []string       `json:"blame_path"`
	Error            string         `json:"error"`
	FaultType        string         `json:"fault_type"`
	ChainDepth       int            `json:"chain_depth"`
	TotalAgents      int            `json:"total_agents"`
	BlameAttribution string         `json:"blame_attribution"`
	Findings         []AgentFinding `json:"findings"`
}

// AgentFinding describes the verification result for a single agent.
type AgentFinding struct {
	Agent     string `json:"agent"`
	Authentic *bool  `json:"authentic"`
	FaultType string `json:"fault_type"`
	ChainOK   *bool  `json:"chain_ok"`
}

// AttackInfo from /api/attacks
type AttackInfo struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	ExpectedFault string `json:"expected_fault"`
}

// AttackResult from /api/attack/simulate
type AttackResult struct {
	AttackType        string       `json:"attack_type"`
	AttackName        string       `json:"attack_name"`
	AttackDescription string       `json:"attack_description"`
	ExpectedFault     string       `json:"expected_fault"`
	Detected          bool         `json:"detected"`
	FaultMatch        bool         `json:"fault_match"`
	Result            VerifyDetail `json:"result"`
}

// BenchmarkResult from /api/benchmark/verify
type BenchmarkResult struct {
	Verification struct {
		Runs    int     `json:"runs"`
		MeanMS  float64 `json:"mean_ms"`
		MedianMS float64 `json:"median_ms"`
		P95MS   float64 `json:"p95_ms"`
	} `json:"verification"`
	BlameAttribution struct {
		Runs   int     `json:"runs"`
		MeanMS float64 `json:"mean_ms"`
	} `json:"blame_attribution"`
	ChainInfo ChainInfo `json:"chain_info"`
}
