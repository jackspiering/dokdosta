package api

import "time"

// APIVersion is the version of the JSON contract served under /api/v1. The documents below are that contract, and the
// future multi-server hub reads them from every agent (plan §3.11): every record carries its host, event IDs are
// monotonic per host, and every time is UTC (RFC 3339). A field is added, never renamed or removed, within v1.
const APIVersion = "v1"

// StatusDoc is GET /api/v1/status.
type StatusDoc struct {
	Host             string           `json:"host"`
	API              string           `json:"api"`
	Version          string           `json:"version"`
	Time             time.Time        `json:"time"`
	StartedAt        time.Time        `json:"started_at"`
	Mode             string           `json:"mode"`
	DryRun           bool             `json:"dry_run"`
	Persistent       bool             `json:"persistent"`
	Healthy          bool             `json:"healthy"`
	HealthDetail     string           `json:"health_detail"`
	EventsUp         bool             `json:"events_up"`
	Settled          bool             `json:"settled"`
	Compose          ComposeDoc       `json:"compose"`
	VersionGate      VersionGateDoc   `json:"version_gate"`
	DriftGuard       string           `json:"drift_guard"`
	RateLimit        RateLimitDoc     `json:"rate_limit"`
	Counts           CountsDoc        `json:"counts"`
	Pauses           []PauseDoc       `json:"pauses"`
	RestoreUntil     time.Time        `json:"restore_window_until,omitzero"`
	LastEventID      uint64           `json:"last_event_id"`
	HistoryRetention int64            `json:"history_retention_seconds"`
	Notifications    NotificationsDoc `json:"notifications"`
	PullAuth         PullAuthDoc      `json:"pull_auth"`
	Store            *StoreDoc        `json:"store,omitempty"` // nil in a dry run, which stores nothing
}

// StoreDoc says whether the store has failed since startup. Healing carries on from memory when it does, but what failed
// to be written does not survive a restart. Only a count, a time and a fixed operation name: never a key or an error text.
type StoreDoc struct {
	Errors      int64     `json:"errors"`
	LastErrorAt time.Time `json:"last_error_at,omitzero"`
	LastErrorOp string    `json:"last_error_op,omitempty"` // "write pause", "read meta", ...
}

// PullAuthDoc says which registries the pull rung has credentials for (WHALENURSE_REGISTRY_AUTH_DIR): the hosts, sorted,
// and nothing else. Never a user name, a secret or the path.
type PullAuthDoc struct {
	Registries []string `json:"registries"`
}

// ComposeDoc says which compose binary whalenurse uses and where it came from.
type ComposeDoc struct {
	Binary  string `json:"binary,omitempty"`
	Source  string `json:"source,omitempty"` // env, host-plugin, bundled, path
	Version string `json:"version,omitempty"`
	Engine  string `json:"engine,omitempty"`
	Arch    string `json:"engine_arch,omitempty"`
}

// VersionGateDoc is the startup version gate (plan §3.6): rungs 2 and 3 and the drift guard need Engine >= 28.1 and
// Compose >= v5.2.0.
type VersionGateDoc struct {
	RecreateEnabled bool     `json:"recreate_enabled"`
	DisabledRungs   []string `json:"disabled_rungs"`
	Reason          string   `json:"reason,omitempty"`
	MinEngine       string   `json:"min_engine"`
	MinCompose      string   `json:"min_compose"`
}

// RateLimitDoc is the global action budget (WHALENURSE_RATE_LIMIT).
type RateLimitDoc struct {
	Limit     int       `json:"limit_per_hour"`
	Used      int       `json:"used_last_hour"`
	Remaining int       `json:"remaining"`
	NextSlot  time.Time `json:"next_slot,omitzero"` // set only while the budget is spent
}

// CountsDoc counts the containers by what whalenurse thinks of them.
type CountsDoc struct {
	Containers int `json:"containers"`
	Watched    int `json:"watched"`
	OK         int `json:"ok"`
	Incident   int `json:"incident"`
	Recovering int `json:"recovering"`
	Suspended  int `json:"suspended"`
	Paused     int `json:"paused"`
}

// PauseDoc is one PAUSED scope.
type PauseDoc struct {
	Host    string    `json:"host"`
	Scope   string    `json:"scope"`
	Created time.Time `json:"created"`
	Until   time.Time `json:"until,omitzero"` // absent: until resumed
}

// NotificationsDoc holds counts only: never a URL, a token or an error text.
type NotificationsDoc struct {
	Enabled     bool     `json:"enabled"`
	URLs        int      `json:"urls"`
	Invalid     int      `json:"invalid"`
	Sent        int64    `json:"sent"`
	Failed      int64    `json:"failed"`
	RateLimited int64    `json:"rate_limited"`
	Dropped     int64    `json:"dropped"`
	UptimePing  *PingDoc `json:"uptime_ping,omitempty"`
}

// PingDoc counts the uptime ping.
type PingDoc struct {
	Sent             int64 `json:"sent"`
	Failed           int64 `json:"failed"`
	SkippedUnhealthy int64 `json:"skipped_unhealthy"`
	IntervalSeconds  int   `json:"interval_seconds"`
}

// ContainerDoc is one row of GET /api/v1/containers and the head of the detail document.
type ContainerDoc struct {
	Host         string            `json:"host"`
	Key          string            `json:"key"`
	Name         string            `json:"name"`
	ID           string            `json:"id"` // the first 12 characters
	Project      string            `json:"project,omitempty"`
	Service      string            `json:"service,omitempty"`
	Number       string            `json:"number,omitempty"`
	Image        string            `json:"image,omitempty"`
	State        string            `json:"state"`            // Docker's status: running, exited, restarting ...
	Health       string            `json:"health,omitempty"` // empty: no healthcheck
	RestartCount int               `json:"restart_count"`
	ExitCode     int               `json:"exit_code"`
	OOMKilled    bool              `json:"oom_killed,omitempty"`
	StartedAt    time.Time         `json:"started_at,omitzero"`
	FinishedAt   time.Time         `json:"finished_at,omitzero"`
	Policy       string            `json:"restart_policy,omitempty"`
	StartError   string            `json:"start_error,omitempty"` // State.Error, redacted and cut
	Watched      bool              `json:"watched"`               // opted in, or a sidecar of an opted-in parent
	OptedIn      bool              `json:"opted_in"`
	Ladder       []string          `json:"ladder,omitempty"`
	HealExited   string            `json:"heal_exited,omitempty"`
	Healer       HealerDoc         `json:"healer"`
	Decision     *DecisionDoc      `json:"decision,omitempty"`
	Parent       *RelationDoc      `json:"parent,omitempty"`
	Dependents   []RelationDoc     `json:"dependents,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"` // com.docker.compose.* and whalenurse.* only
}

// HealerDoc is whalenurse's state for the container.
type HealerDoc struct {
	State          string       `json:"state"` // ok, incident, recovering, SUSPENDED, PAUSED
	Class          string       `json:"class,omitempty"`
	IncidentOpened time.Time    `json:"incident_opened,omitzero"`
	RungsRun       int          `json:"rungs_run,omitempty"`
	NextRung       string       `json:"next_rung,omitempty"`     // empty when the ladder is spent
	SkippedRungs   []SkippedDoc `json:"skipped_rungs,omitempty"` // rungs of this incident that were not available, and so did not run
	CooldownUntil  time.Time    `json:"cooldown_until,omitzero"`
	RecoverySince  time.Time    `json:"recovery_since,omitzero"`
	Flaps          int          `json:"flaps"`
	FlapLimit      int          `json:"flap_limit"`
	ManualStop     bool         `json:"manual_stop,omitempty"`
	LoopIncreases  int          `json:"loop_increases,omitempty"`
	Suspended      *SuspendDoc  `json:"suspended,omitempty"`
	Paused         *PausedDoc   `json:"paused,omitempty"`
}

// SkippedDoc is a rung of the open incident that could not run: the version gate, no usable compose inputs, a
// pull_policy that forbids the pull. It counts toward RungsRun (the ladder moved past it) but nothing was done.
type SkippedDoc struct {
	Rung   string `json:"rung"`
	Reason string `json:"reason"`
}

// SuspendDoc says why a container is SUSPENDED. Cause is the sentence of the "giving up" message (detail only).
type SuspendDoc struct {
	Reason string    `json:"reason"` // ladder, flapping, drift
	Since  time.Time `json:"since,omitzero"`
	Cause  string    `json:"cause,omitempty"`
}

// PausedDoc says which pause covers the container and when it ends.
type PausedDoc struct {
	Scope string    `json:"scope"`
	Since time.Time `json:"since,omitzero"`
	Until time.Time `json:"until,omitzero"` // absent: until resumed
}

// DecisionDoc is the last classification of the container by the rules (plan §3.3).
type DecisionDoc struct {
	Class  string    `json:"class"`
	Action string    `json:"action,omitempty"`
	Skip   string    `json:"skip,omitempty"`
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

// RelationDoc names the other end of a network_mode: container:<parent> link.
type RelationDoc struct {
	Host        string `json:"host"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	State       string `json:"state,omitempty"`        // Docker's status of the other container
	Health      string `json:"health,omitempty"`       // empty: no healthcheck
	HealerState string `json:"healer_state,omitempty"` // ok, incident, recovering, SUSPENDED, PAUSED
}

// ContainerDetailDoc is GET /api/v1/containers/{key}.
type ContainerDetailDoc struct {
	ContainerDoc
	Timeline    []EventDoc    `json:"timeline"` // newest first
	Drift       DriftDoc      `json:"drift"`
	LastUpgrade *UpgradeDoc   `json:"last_upgrade,omitempty"`
	Incidents   []IncidentDoc `json:"incidents"`
}

// DriftDoc is the drift guard's last word about the container. The guard runs right before a recreate, not on a
// timer, so this is what it found then.
type DriftDoc struct {
	Status   string    `json:"status"` // blocked, was-blocked, warned, none-recorded
	Blocking bool      `json:"blocking"`
	At       time.Time `json:"at,omitzero"`
	Findings string    `json:"findings,omitempty"`
	MovedBy  string    `json:"moved_by,omitempty"` // the container whose pull rung moved the tag
	MovedAt  time.Time `json:"moved_at,omitzero"`
	Note     string    `json:"note"`
}

// UpgradeDoc is the last "upgraded during heal" of the container, with the rollback commands.
type UpgradeDoc struct {
	EventID     uint64    `json:"event_id"`
	Kind        string    `json:"kind"` // TagMoved or Upgraded
	At          time.Time `json:"at"`
	Ref         string    `json:"ref,omitempty"`
	OldImage    string    `json:"old_image,omitempty"`
	NewImage    string    `json:"new_image,omitempty"`
	OK          bool      `json:"ok"`
	Error       string    `json:"error,omitempty"`
	Sidecars    []string  `json:"sidecars,omitempty"`
	RollbackTag string    `json:"rollback_tag,omitempty"`
	RollbackUp  string    `json:"rollback_up,omitempty"`
	Message     string    `json:"message"`
}

// EventDoc is one domain event. ID is the per-host cursor of GET /api/v1/events?since=.
type EventDoc struct {
	ID        uint64            `json:"id"`
	Time      time.Time         `json:"time"`
	Host      string            `json:"host"`
	Key       string            `json:"key"`
	Container string            `json:"container,omitempty"`
	Type      string            `json:"type"`
	Message   string            `json:"message"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

// EventsDoc is GET /api/v1/events. Next is the cursor to ask for next. Reset is set when the cursor the caller sent
// is ahead of this host's last event (the store was wiped, or a dry run restarted): the events returned then start
// from the beginning and the caller should discard what it holds for the host.
type EventsDoc struct {
	Host   string     `json:"host"`
	Events []EventDoc `json:"events"`
	Next   uint64     `json:"next"`
	LastID uint64     `json:"last_id"`
	Reset  bool       `json:"reset,omitempty"`
}

// IncidentDoc is one incident, open or closed.
type IncidentDoc struct {
	Host    string    `json:"host"`
	Key     string    `json:"key"`
	Opened  time.Time `json:"opened"`
	Closed  time.Time `json:"closed,omitzero"`
	Open    bool      `json:"open"`
	Class   string    `json:"class,omitempty"`
	State   string    `json:"state,omitempty"` // an open incident: incident, recovering, SUSPENDED
	Outcome string    `json:"outcome,omitempty"`
}

// IncidentsDoc is GET /api/v1/incidents.
type IncidentsDoc struct {
	Host      string        `json:"host"`
	Incidents []IncidentDoc `json:"incidents"` // open ones first, then closed, newest first
}

// ContainersDoc is GET /api/v1/containers.
type ContainersDoc struct {
	Host       string         `json:"host"`
	Containers []ContainerDoc `json:"containers"`
}
