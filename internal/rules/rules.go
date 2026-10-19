// Package rules classifies a container from a fresh inspect, following the
// precedence in plan §3.3. It is pure: no Docker calls, no clock (the caller
// passes Now), no state. Events are inputs only through Evidence.
package rules

import (
	"fmt"
	"strings"
	"time"

	"github.com/jackspiering/whalenurse/internal/config"
	"github.com/jackspiering/whalenurse/internal/docker"
)

// Action is what the rules want done.
type Action string

const (
	ActionNone          Action = "none"
	ActionRestart       Action = "restart"         // API restart: running, unhealthy, crash-looping, or a dependent whose parent restarted later
	ActionStart         Action = "start"           // API start: created or exited
	ActionRecreate      Action = "recreate"        // compose --force-recreate, skipping the restart rung that cannot help (a network that is gone)
	ActionRecreateStale Action = "recreate-stale"  // a dependent whose parent was recreated: compose up without --force-recreate (plan §3.5)
	ActionPull          Action = "pull"            // rung 3 (opt-in): compose pull, then an up if the image changed; only the ladder plans it, never the rules
	ActionNeedsRecreate Action = "needs-recreate"  // advisory: a recreate is what it needs, but none is available (version gate, no usable compose labels)
	ActionWaitForParent Action = "wait-for-parent" // act only on the parent
	ActionNotify        Action = "notify"          // log + domain event, no action
)

// IsRecreate reports whether the action is carried out by compose rather than the Docker API.
func (a Action) IsRecreate() bool { return a == ActionRecreate || a == ActionRecreateStale }

// Class names the condition found. Skip reasons use ClassSkip plus Skip.
type Class string

const (
	ClassSkip                 Class = "skip"
	ClassOK                   Class = "ok"
	ClassStarting             Class = "starting" // healthcheck still starting
	ClassUnhealthy            Class = "unhealthy"
	ClassRestarting           Class = "restarting" // restarting, not yet long enough to be a crash loop
	ClassCrashLoop            Class = "crash-loop" // restarting longer than CRASHLOOP_AFTER
	ClassRetriesExhausted     Class = "retries-exhausted"
	ClassNeverStarted         Class = "never-started"
	ClassStartFailed          Class = "start-failed" // State.Error is set
	ClassExitedCrashed        Class = "exited-crashed"
	ClassExitedOOM            Class = "exited-oom"
	ClassExitedAlways         Class = "exited-always" // heal-exited=always
	ClassExitedIntended       Class = "exited-intended"
	ClassExitedManual         Class = "exited-manual"       // a kill was seen: someone stopped it
	ClassExitedClean          Class = "exited-clean"        // exit 0 under an always-style policy
	ClassExitedSignalled      Class = "exited-signalled"    // 137 or 143 without OOM
	ClassExitedNever          Class = "exited-never"        // heal-exited=never
	ClassExitedHostStopped    Class = "exited-host-stopped" // stopped by a daemon shutdown or reboot, restart policy "no": stays down (notify)
	ClassExitedHostRestore    Class = "exited-host-restore" // stopped by a shutdown, policy restarts it: left to Docker's restore
	ClassDead                 Class = "dead"
	ClassStaleParentGone      Class = "stale-parent-gone"
	ClassStaleParentRestarted Class = "stale-parent-restarted"
	ClassWaitingForParent     Class = "waiting-for-parent"
	ClassParentUnresolved     Class = "parent-unresolved"
)

// Skip reasons (rule 0).
const (
	SkipSelf        = "self"
	SkipOneOff      = "oneoff"
	SkipSwarm       = "swarm"
	SkipNotOptedIn  = "not-opted-in"
	SkipDockerPause = "docker-paused"
	SkipRemoving    = "removing"
)

// Reference kinds for Parent.
const (
	RefNetwork = "network"
	RefIPC     = "ipc"
	RefPID     = "pid"
)

// Parent is the resolved target of one container:<ref> mode of the container
// being classified. The caller resolves it; the rules never look anything up.
type Parent struct {
	Kind      string
	Ref       string
	Found     bool
	ID        string
	Running   bool
	StartedAt time.Time
	Busy      bool // the parent has an open incident or an action in flight
}

// Input is everything Classify may look at.
type Input struct {
	Now      time.Time
	C        docker.Container
	Cfg      config.Container
	Self     bool
	Parents  []Parent // one per container: reference in network_mode, ipc or pid
	Evidence Evidence
	Loop     Loop // the crash-loop state the engine derived from RestartCount (rule 3)
	// DependentOfOptedIn: a container: parent of this container (found, or named by its depends_on label when its ID is
	// gone) is opted in. Such a container is repaired by the dependents step even when it is not opted in itself, unless
	// it is labelled whalenurse.enable=false; its own failures are still its own business (plan v3.6).
	DependentOfOptedIn bool
	// HostStopped: the container stopped inside a shutdown window (plan §3.3 rule 4): between 30 s before the
	// previous run's last_seen, or the events-stream loss, and the daemon's restore. Whatever its exit code.
	HostStopped bool
}

// Loop is the verdict of the crash-loop tracker (internal/engine): at least 3
// RestartCount increases, the first at least CRASHLOOP_AFTER ago, and not yet
// cleared by RECOVERY_PERIOD without a new increase. The rules do not compute it
// because it needs memory between inspects; they only act on it.
type Loop struct {
	Active    bool
	Since     time.Time // the first counted increase
	Increases int
}

// Decision is the result of Classify.
type Decision struct {
	Class  Class
	Action Action
	Cause  Cause  // set when Class is ClassStartFailed
	Skip   string // set when Class is ClassSkip
	Reason string
	// Dependents names the containers that depend on this one, for the single notification about a parent that stays
	// down (rule 4): the daemon fills it in, the rules never look the others up.
	Dependents []string
}

func (d Decision) String() string {
	return fmt.Sprintf("%s/%s", d.Class, d.Action)
}

// Exit codes with a meaning for the rules.
const (
	exitSigkill = 137
	exitSigterm = 143
)

// Classify applies the §3.3 precedence to one inspected container.
func Classify(in Input) Decision {
	c := in.C
	// Rule 0: skip entirely.
	switch {
	case in.Self:
		return skip(SkipSelf, "whalenurse's own container")
	case strings.EqualFold(c.Labels[docker.LabelOneOff], "true"):
		return skip(SkipOneOff, "compose run container")
	case c.Labels[docker.LabelSwarmTaskID] != "":
		return skip(SkipSwarm, "swarm task")
	case in.Cfg.Disabled:
		return skip(SkipNotOptedIn, "whalenurse.enable=false")
	case !in.Cfg.OptedIn && !in.DependentOfOptedIn:
		return skip(SkipNotOptedIn, "not opted in")
	case c.Status == "removing":
		return skip(SkipRemoving, "being removed")
	case c.Paused || c.Status == "paused":
		return skip(SkipDockerPause, "paused with docker pause")
	}
	d := guardReferences(in, classifyState(in))
	if !in.Cfg.OptedIn && d.Class == ClassUnhealthy && d.Action == ActionRestart {
		// Not opted in: an unhealthy sidecar is its own business, unless it is unhealthy because its parent restarted
		// after it (the namespace it holds is stale); then the dependents step restarts it.
		for _, p := range in.Parents {
			if p.Found && p.Running && !p.Busy && !p.StartedAt.IsZero() && p.StartedAt.After(c.StartedAt) {
				d = Decision{Class: ClassStaleParentRestarted, Action: ActionRestart,
					Reason: fmt.Sprintf("parent %s started after this container; its namespace is stale; restarting it", docker.ShortID(p.ID))}
				break
			}
		}
	}
	if !in.Cfg.OptedIn && !d.IsDependentStep() {
		// Not opted in, but its parent is: only the dependents step may touch it.
		return skip(SkipNotOptedIn, "not opted in (only the dependents step of an opted-in parent repairs it)")
	}
	return d
}

// IsDependentStep reports whether the decision is the dependents step of plan §3.5: a container made stale by its parent
// being restarted or recreated. It is repaired as part of the parent's incident, so it does not count toward the
// container's own flap limit, and it needs no opt-in of its own.
func (d Decision) IsDependentStep() bool {
	return d.Action == ActionRecreateStale || d.Class == ClassStaleParentRestarted
}

func skip(reason, text string) Decision {
	return Decision{Class: ClassSkip, Action: ActionNone, Skip: reason, Reason: text}
}

func classifyState(in Input) Decision {
	c := in.C

	// Rule 1: a non-empty State.Error is a failed start. It overrides every
	// rule below and the exit code is never read: a failed first start is
	// `created` with 126/127/128, a failed restart-policy restart is `exited`
	// with the previous run's code.
	if c.Error != "" && (c.Status == "created" || c.Status == "exited") {
		cause := ClassifyStartError(c.Error)
		d := Decision{Class: ClassStartFailed, Action: ActionStart, Cause: cause, Reason: "failed start (" + string(cause) + "): " + trim(c.Error)}
		switch cause {
		case CauseParentGone:
			// Restarting cannot work: the parent it referenced no longer exists. Recreate the dependent (plan §3.5).
			d.Action = ActionRecreateStale
		case CauseNetworkGone:
			// Restarting cannot work either; the recreate is rung 2 of an ordinary ladder, the drift guard included.
			d.Action = ActionRecreate
		}
		// CauseParentStopped stays a Start; guardReferences turns it into
		// "wait for the parent" until the parent runs, then it is a plain start.
		return d
	}

	switch c.Status {
	case "dead":
		return Decision{Class: ClassDead, Action: ActionNotify, Reason: "container is dead; it cannot be started"}
	case "created":
		return Decision{Class: ClassNeverStarted, Action: ActionStart, Reason: "created but never started"}
	case "restarting":
		return classifyRestarting(in)
	case "running":
		return classifyRunning(in)
	case "exited":
		return classifyExited(in)
	}
	return Decision{Class: ClassOK, Action: ActionNone, Reason: "status " + c.Status}
}

func classifyRunning(in Input) Decision {
	if in.C.Health == "unhealthy" {
		return Decision{Class: ClassUnhealthy, Action: ActionRestart, Reason: "healthcheck reports unhealthy"}
	}
	// A loop that crashes every ~15s is mostly `running`: the engine resets its
	// restart backoff after a run of 10s, so it is rarely `restarting` for long.
	if in.Loop.Active {
		return crashLoop(in)
	}
	switch in.C.Health {
	case "starting":
		return Decision{Class: ClassStarting, Action: ActionNone, Reason: "healthcheck still starting"}
	}
	return Decision{Class: ClassOK, Action: ActionNone, Reason: "running"}
}

// Rule 3: a crash loop is RestartCount rising, not the `restarting` status, so a
// container that is `restarting` and not (yet) a loop is only watched.
func classifyRestarting(in Input) Decision {
	if !in.Loop.Active {
		return Decision{Class: ClassRestarting, Action: ActionNone, Reason: "restarting; not (yet) a crash loop"}
	}
	return crashLoop(in)
}

func crashLoop(in Input) Decision {
	return Decision{Class: ClassCrashLoop, Action: ActionRestart,
		Reason: fmt.Sprintf("crash loop for %s (%d restarts counted, restart count %d)",
			in.Now.Sub(in.Loop.Since).Round(time.Second), in.Loop.Increases, in.C.RestartCount)}
}

// Rules 3 (on-failure exhausted) and 4 (exited without State.Error).
func classifyExited(in Input) Decision {
	c, ev := in.C, in.Evidence
	signalled := c.ExitCode == exitSigkill || c.ExitCode == exitSigterm

	// Stopped by the host (a daemon restart or a reboot): the exit code says nothing, because an app that handles
	// SIGTERM may exit 1 or 0. The restart policy decides. "no" stays down, with one notification; the policies
	// Docker restores are left to its restore (a failed restore has State.Error, so rule 1 handled it). Only
	// heal-exited=always starts it.
	if in.HostStopped {
		switch {
		case in.Cfg.HealExited == config.HealAlways:
			return Decision{Class: ClassExitedAlways, Action: ActionStart, Reason: "heal-exited=always"}
		case c.RestartPolicy == "no" || c.RestartPolicy == "":
			return Decision{Class: ClassExitedHostStopped, Action: ActionNotify,
				Reason: fmt.Sprintf("stopped by a daemon shutdown or reboot (exit %d) and restart policy is no: staying down", c.ExitCode)}
		}
		return Decision{Class: ClassExitedHostRestore, Action: ActionNone,
			Reason: fmt.Sprintf("stopped by a daemon shutdown or reboot (exit %d): Docker's restore restarts it (policy %s)", c.ExitCode, c.RestartPolicy)}
	}

	if c.RestartPolicy == "on-failure" && c.MaxRetries > 0 && c.RestartCount >= c.MaxRetries &&
		c.ExitCode != 0 && !signalled && !ev.ManualStop {
		return Decision{Class: ClassRetriesExhausted, Action: ActionStart,
			Reason: fmt.Sprintf("on-failure:%d exhausted (exit %d)", c.MaxRetries, c.ExitCode)}
	}

	// heal-exited=always heals any exited container.
	if in.Cfg.HealExited == config.HealAlways {
		return Decision{Class: ClassExitedAlways, Action: ActionStart, Reason: "heal-exited=always"}
	}

	// Intended exit: policy no or on-failure with exit code 0. Left alone in
	// `crashed` and `never` modes.
	if c.ExitCode == 0 && (c.RestartPolicy == "no" || c.RestartPolicy == "" || c.RestartPolicy == "on-failure") {
		return Decision{Class: ClassExitedIntended, Action: ActionNone, Reason: "exited 0 under restart policy " + policyName(c)}
	}

	switch in.Cfg.HealExited {
	case config.HealNever:
		return Decision{Class: ClassExitedNever, Action: ActionNotify, Reason: fmt.Sprintf("exited (%d); heal-exited=never", c.ExitCode)}
	}

	// heal-exited=crashed.
	switch {
	case c.OOMKilled:
		return Decision{Class: ClassExitedOOM, Action: ActionStart, Reason: "killed by the OOM killer"}
	case ev.ManualStop:
		return Decision{Class: ClassExitedManual, Action: ActionNone, Reason: "a kill was seen before the exit: stopped by hand"}
	case ev.Crashed:
		return Decision{Class: ClassExitedCrashed, Action: ActionStart, Reason: fmt.Sprintf("died with exit %d, no preceding kill", c.ExitCode)}
	}
	// No events seen (whalenurse was down): fall back to the exit code.
	switch {
	case c.ExitCode == 0:
		return Decision{Class: ClassExitedClean, Action: ActionNone, Reason: "exited 0 under restart policy " + policyName(c)}
	case signalled:
		return Decision{Class: ClassExitedSignalled, Action: ActionNotify,
			Reason: fmt.Sprintf("exit %d without OOM: stopped by a signal, not healing", c.ExitCode)}
	}
	return Decision{Class: ClassExitedCrashed, Action: ActionStart, Reason: fmt.Sprintf("exited with code %d", c.ExitCode)}
}

func policyName(c docker.Container) string {
	if c.RestartPolicy == "" {
		return "no"
	}
	return c.RestartPolicy
}

// guardReferences enforces plan §3.5 for containers that share a namespace
// with another container (network_mode/ipc/pid: container:<x>):
//   - parent ID gone: never restart or start it. That fails and turns a
//     running-but-disconnected dependent into an exited one (E1). It is recreated
//     with a non-forced `up` (ActionRecreateStale).
//   - parent not running, or busy with its own incident: act only on the parent.
//   - a running dependent whose parent started later is stale: restart it through
//     the API (ClassStaleParentRestarted), the dependents step.
func guardReferences(in Input, d Decision) Decision {
	if in.C.Status == "removing" {
		return d
	}
	refs := 0
	for _, m := range []string{in.C.NetworkMode, in.C.IpcMode, in.C.PidMode} {
		if docker.ContainerRef(m) != "" {
			refs++
		}
	}
	if refs == 0 {
		return d
	}
	if len(in.Parents) < refs {
		// A container: reference we could not resolve: do nothing rather than guess.
		if d.Action == ActionRestart || d.Action == ActionStart {
			return Decision{Class: ClassParentUnresolved, Action: ActionNone, Cause: d.Cause, Reason: "could not resolve the container: reference; not acting"}
		}
		return d
	}

	for _, p := range in.Parents {
		if !p.Found {
			// Parent ID gone. Regardless of what else is wrong, an API restart/start
			// is forbidden, and a running dependent is silently stale.
			if d.Action == ActionRecreateStale {
				return d // rule 1 already said so, with its own class and cause
			}
			if d.Action == ActionRestart || d.Action == ActionStart || d.Action.IsRecreate() ||
				(d.Action == ActionNone && in.C.Running && (d.Class == ClassOK || d.Class == ClassStarting)) {
				return Decision{Class: ClassStaleParentGone, Action: ActionRecreateStale, Cause: d.Cause,
					Reason: fmt.Sprintf("%s: container:%s no longer exists (parent was recreated or removed); recreating it, never restarting", p.Kind, p.Ref)}
			}
			return d
		}
	}
	switch {
	case d.Action == ActionRestart || d.Action == ActionStart || d.Action.IsRecreate():
		for _, p := range in.Parents {
			if !p.Running || p.Busy {
				return waitForParent(d, p)
			}
		}
	case d.Action == ActionNone && in.C.Running && d.Class == ClassOK:
		for _, p := range in.Parents {
			if p.Running && !p.StartedAt.IsZero() && p.StartedAt.After(in.C.StartedAt) {
				if p.Busy {
					return waitForParent(d, p)
				}
				return Decision{Class: ClassStaleParentRestarted, Action: ActionRestart,
					Reason: fmt.Sprintf("parent %s started after this container; its namespace is stale; restarting it", docker.ShortID(p.ID))}
			}
		}
	}
	return d
}

func waitForParent(d Decision, p Parent) Decision {
	why := "is not running"
	if p.Busy {
		why = "has an incident open or an action in flight"
	}
	return Decision{Class: ClassWaitingForParent, Action: ActionWaitForParent, Cause: d.Cause,
		Reason: fmt.Sprintf("parent %s %s; acting on the parent only", docker.ShortID(p.ID), why)}
}

func trim(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}
