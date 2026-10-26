package daemon

import (
	"context"
	"strings"

	"github.com/jackspiering/whalenurse/internal/compose"
	"github.com/jackspiering/whalenurse/internal/docker"
)

// The depends_on conditions compose can hold a container back on (compose-go types.ServiceCondition). service_started holds
// nothing that matters here: `up` starts the dependency first and does not wait for more.
const (
	condHealthy   = "service_healthy"
	condCompleted = "service_completed_successfully"
)

// waitsFor says what compose is holding a `created` container back on (plan v3.13 item 14), or "" when nothing holds it.
// `up` creates every container first and runs waitDependencies before it starts each one, so a created container whose
// dependency is not healthy yet (service_healthy) or has not exited 0 yet (service_completed_successfully) is waiting, not
// failed to start. The condition is read from the com.docker.compose.depends_on label (`<svc>:<condition>:<restart>`),
// and the dependency's containers are the others of the same project. A dependency with no container at all is not
// waited for (compose has nothing to wait on), and one that cannot be read is waited for: the container is looked at
// again on the next poll.
func (d *Daemon) waitsFor(ctx context.Context, c docker.Container) string {
	if c.Status != "created" || c.Error != "" {
		return ""
	}
	project := c.Labels[docker.LabelProject]
	if project == "" {
		return ""
	}
	var waits []string
	for _, e := range strings.Split(c.Labels[compose.LabelDependsOn], ",") {
		parts := strings.Split(strings.TrimSpace(e), ":")
		if len(parts) < 2 || (parts[1] != condHealthy && parts[1] != condCompleted) {
			continue
		}
		svc, cond := parts[0], parts[1]
		holds, found := false, false
		for _, s := range d.idx.all {
			if s.Labels[docker.LabelProject] != project || s.Labels[docker.LabelService] != svc || docker.IsOneOff(s.Labels) || s.ID == c.ID {
				continue
			}
			found = true
			switch cond {
			case condHealthy:
				holds = holds || s.Health != "healthy"
			case condCompleted:
				if s.State != "exited" {
					holds = true
					break
				}
				dep, err := d.rd.Inspect(ctx, s.ID)
				holds = holds || err != nil || dep.ExitCode != 0
			}
		}
		if found && holds {
			waits = append(waits, svc+" ("+cond+")")
		}
	}
	return strings.Join(waits, ", ")
}
