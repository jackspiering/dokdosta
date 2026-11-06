package daemon

import (
	"github.com/jackspiering/dokdosta/internal/docker"
	"github.com/jackspiering/dokdosta/internal/rules"
)

// nsRef is one container: reference of a container: its namespace kind (rules.RefNetwork, RefIPC, RefPID) and the raw
// reference (an ID or a name).
type nsRef struct{ Kind, Ref string }

// nsRefs are the container: references of c, in the order network_mode, ipc, pid: the one walk over them (WN-023). The
// learned "kind:service" entries (learnedParents) use these kinds, so they must not change.
func nsRefs(c docker.Container) []nsRef {
	var out []nsRef
	for _, r := range []nsRef{{rules.RefNetwork, c.NetworkMode}, {rules.RefIPC, c.IpcMode}, {rules.RefPID, c.PidMode}} {
		if ref := docker.ContainerRef(r.Ref); ref != "" {
			out = append(out, nsRef{r.Kind, ref})
		}
	}
	return out
}

// containerRefs lists the container: references of a container, raw: network_mode, ipc and pid.
func containerRefs(c docker.Container) []string {
	out := []string{}
	for _, r := range nsRefs(c) {
		out = append(out, r.Ref)
	}
	return out
}

// serviceContainers are the containers of the last list that belong to this compose service of this project, one-offs
// (`compose run`) left out.
func (ix index) serviceContainers(project, service string) []docker.Summary {
	var out []docker.Summary
	for _, s := range ix.all {
		if s.Labels[docker.LabelProject] == project && s.Labels[docker.LabelService] == service && !docker.IsOneOff(s.Labels) {
			out = append(out, s)
		}
	}
	return out
}
