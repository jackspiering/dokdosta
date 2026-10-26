package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/jackspiering/whalenurse/internal/compose"
	"github.com/jackspiering/whalenurse/internal/docker"
	"github.com/jackspiering/whalenurse/internal/engine"
	"github.com/jackspiering/whalenurse/internal/rules"
)

// created puts a container in the state `docker compose up` leaves it in before it starts it.
func (h *cfH) created(id, project, service, dependsOn string) docker.Container {
	h.t.Helper()
	h.svc(id, project, service, compose.LabelDependsOn, dependsOn)
	return h.set(id, func(c *docker.Container) { c.Status, c.Running, c.StartedAt = "created", false, time.Time{} })
}

// Plan v3.13 item 14 (DQ7): compose creates every container first and starts each one when its dependencies hold. A
// created container whose depends_on condition does not hold yet is held, with no incident; it starts when it holds.
func TestCreatedContainerHeldBackByServiceHealthyIsNotStarted(t *testing.T) {
	h := newCompose(t, nil, nil, docker.ServerInfo{})
	db := h.svc(hexID("d1"), "p", "db")
	h.set(db.ID, func(c *docker.Container) { c.Health = "starting" })
	web := h.created(hexID("a1"), "p", "web", "db:service_healthy:true")
	h.tick()
	h.advance(5 * time.Minute)
	h.wantCalls()
	if h.count(engine.IncidentOpened) != 0 {
		t.Fatalf("an incident was opened for a container compose is holding back: %v", h.rec.Types())
	}
	held := h.d.Status().(Status).Held
	if len(held) != 1 || held[0].Container != web.Name || held[0].WaitingFor != "db (service_healthy)" {
		t.Fatalf("status should say what it waits for: %+v", held)
	}
	_, seen := h.d.view.snapshot()
	var dec rules.Decision
	for _, s := range seen {
		if s.c.ID == web.ID {
			dec = s.dec
		}
	}
	if dec.Class != rules.ClassHeldBack || !strings.Contains(dec.Reason, "waiting for db (service_healthy)") {
		t.Fatalf("the dashboard shows the decision: %+v", dec)
	}

	// The condition holds: the container is looked at again and, being created and never started, the ladder starts it.
	h.set(db.ID, healthy)
	h.advance(2 * time.Minute)
	h.wantCalls("start " + web.ID)
	if got := h.d.Status().(Status).Held; len(got) != 0 {
		t.Fatalf("still held: %+v", got)
	}
}

func TestCreatedContainerHeldBackUntilADependencyCompletesSuccessfully(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*docker.Container)
		held bool
	}{
		{"still running", func(c *docker.Container) {}, true},
		{"exited 1", func(c *docker.Container) { c.Status, c.Running, c.ExitCode = "exited", false, 1 }, true},
		{"exited 0", func(c *docker.Container) { c.Status, c.Running, c.ExitCode = "exited", false, 0 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCompose(t, nil, nil, docker.ServerInfo{})
			mig := h.svc(hexID("d1"), "p", "migrate")
			h.set(mig.ID, tc.set)
			web := h.created(hexID("a1"), "p", "web", "migrate:service_completed_successfully:false")
			h.tick()
			h.advance(5 * time.Minute)
			if tc.held {
				for _, call := range h.fake.Calls() {
					if call.ID == web.ID {
						t.Fatalf("the held container was touched: %v", h.fake.Calls())
					}
				}
				if held := h.d.Status().(Status).Held; len(held) != 1 || held[0].WaitingFor != "migrate (service_completed_successfully)" {
					t.Fatalf("%+v", held)
				}
			} else if calls := h.fake.Calls(); len(calls) != 1 || calls[0].ID != web.ID || calls[0].Verb != "start" {
				t.Fatalf("want a start of the container: %v", calls)
			}
		})
	}
}

// Anything else is not held: service_started, a dependency that has no container, a failed start (State.Error), a label
// that names no condition.
func TestCreatedContainerIsStartedWhenNothingHoldsIt(t *testing.T) {
	for name, tc := range map[string]struct {
		dependsOn string
		prep      func(h *cfH)
		err       string
	}{
		"service_started":             {dependsOn: "db:service_started:true", prep: func(h *cfH) { h.svc(hexID("d1"), "p", "db") }},
		"dependency has no container": {dependsOn: "db:service_healthy:true", prep: func(h *cfH) {}},
		"no label":                    {dependsOn: "", prep: func(h *cfH) {}},
		"dependency is healthy":       {dependsOn: "db:service_healthy:true", prep: func(h *cfH) { db := h.svc(hexID("d1"), "p", "db"); h.set(db.ID, healthy) }},
		"a failed start":              {dependsOn: "db:service_healthy:true", prep: func(h *cfH) { h.svc(hexID("d1"), "p", "db") }, err: "driver failed programming external connectivity: port is already allocated"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newCompose(t, nil, nil, docker.ServerInfo{})
			tc.prep(h)
			web := h.created(hexID("a1"), "p", "web", tc.dependsOn)
			if tc.err != "" {
				h.set(web.ID, func(c *docker.Container) { c.Error, c.ExitCode = tc.err, 128 })
			}
			h.tick()
			h.advance(2 * time.Minute)
			if got := h.d.Status().(Status).Held; len(got) != 0 {
				t.Fatalf("held: %+v", got)
			}
			if len(h.fake.Calls()) == 0 {
				t.Fatalf("not started: %v", h.rec.Types())
			}
		})
	}
}
