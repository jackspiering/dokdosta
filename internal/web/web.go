// Package web is the read-only dashboard: html/template pages and two embedded assets, no Node build, no CDN, no
// inline script or style (the Content-Security-Policy forbids both). It renders the documents of internal/api and
// nothing else: it imports no engine, store or daemon code, so a page can only show what the API says, and the API
// is where the output rules (no env vars, no full inspect, allow-listed labels, redaction) are enforced.
package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackspiering/whalenurse/internal/api"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Handler serves the dashboard: "/", "/containers", "/containers/{key...}", "/history" and "/static/". Mount it on the
// TCP listener behind api.Secure, which adds the headers, refuses every method but GET and HEAD and checks the token.
func Handler(svc *api.Service) http.Handler {
	pages := map[string]*template.Template{}
	for _, name := range []string{"overview", "containers", "container", "history", "error"} {
		pages[name] = template.Must(template.New(name).Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/partials.html", "templates/"+name+".html"))
	}
	h := &handler{svc: svc, pages: pages}

	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.overview)
	mux.HandleFunc("GET /containers", h.containers)
	mux.HandleFunc("GET /containers/{key...}", h.container)
	mux.HandleFunc("GET /history", h.history)
	fileServer := http.StripPrefix("/static/", http.FileServerFS(static))
	mux.HandleFunc("GET /static/{file...}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, max-age=300")
		fileServer.ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.fail(w, http.StatusNotFound, "Not found", "There is no such page.")
	})
	return mux
}

type handler struct {
	svc   *api.Service
	pages map[string]*template.Template
}

// page is the data every template gets; the page fills the rest.
type page struct {
	Title  string
	Page   string
	Host   string
	Status api.StatusDoc

	// overview
	Attention []api.ContainerDoc
	Events    []api.EventDoc

	// containers
	All        bool
	Containers []api.ContainerDoc

	// container
	Detail *api.ContainerDetailDoc
	Scope  string // project/service, as `whalenurse unsuspend` takes it

	// history
	Incidents  []api.IncidentDoc
	Filters    []filter
	Key        string
	Retention  string
	Persistent bool

	// error
	Heading string
	Message string
}

type filter struct {
	Label  string
	Href   string
	Active bool
}

func (h *handler) base(name, title string) page {
	return page{Title: title, Page: name, Host: h.svc.Host, Status: h.svc.Status()}
}

func (h *handler) render(w http.ResponseWriter, name string, code int, p page) {
	var buf bytes.Buffer
	if err := h.pages[name].ExecuteTemplate(&buf, "layout", p); err != nil {
		http.Error(w, "page error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = buf.WriteTo(w)
}

func (h *handler) fail(w http.ResponseWriter, code int, heading, msg string) {
	p := h.base("error", heading)
	p.Heading, p.Message = heading, msg
	h.render(w, "error", code, p)
}

func (h *handler) overview(w http.ResponseWriter, _ *http.Request) {
	p := h.base("overview", "Overview")
	for _, c := range h.svc.Containers(false) {
		if c.Healer.State != "ok" || c.Healer.Paused != nil {
			p.Attention = append(p.Attention, c)
		}
	}
	p.Events = h.svc.EventsPage(api.EventsQuery{Newest: true, Limit: 10}).Events
	h.render(w, "overview", http.StatusOK, p)
}

func (h *handler) containers(w http.ResponseWriter, r *http.Request) {
	p := h.base("containers", "Containers")
	p.All = r.URL.Query().Get("all") == "1"
	p.Containers = h.svc.Containers(p.All)
	h.render(w, "containers", http.StatusOK, p)
}

func (h *handler) container(w http.ResponseWriter, r *http.Request) {
	d, ok := h.svc.Container(r.PathValue("key"))
	if !ok {
		h.fail(w, http.StatusNotFound, "No such container", "whalenurse has not seen a container with that key since it started.")
		return
	}
	p := h.base("container", d.Name)
	p.Detail = &d
	if d.Project != "" {
		p.Scope = d.Project + "/" + d.Service
	} else {
		p.Scope = strings.TrimPrefix(d.Key, d.Host+"/")
	}
	h.render(w, "container", http.StatusOK, p)
}

// eventGroups are the filter chips of the history page.
var eventGroups = []struct {
	label string
	types []string
}{
	{"All", nil},
	{"Actions", []string{"ActionResult", "ActionStarted"}},
	{"Giving up", []string{"Suspended", "Unsuspended"}},
	{"Recoveries", []string{"Recovered", "IncidentClosed"}},
	{"Drift", []string{"DriftBlocked", "DriftWarned"}},
	{"Upgrades", []string{"TagMoved", "Upgraded"}},
	{"Pauses", []string{"Paused", "Resumed"}},
}

func (h *handler) history(w http.ResponseWriter, r *http.Request) {
	p := h.base("history", "History")
	q, err := api.ParseEventsQuery(r)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	q.Newest, q.Limit, q.Since = true, 200, 0
	p.Key = q.Key
	p.Events = h.svc.EventsPage(q).Events
	p.Incidents = h.svc.Incidents(q.Key, 100).Incidents
	p.Persistent = p.Status.Persistent
	if s := p.Status.HistoryRetention; s > 0 {
		p.Retention = (time.Duration(s) * time.Second).Round(time.Hour).String()
		if days := s / 86400; days > 0 && s%86400 == 0 {
			p.Retention = fmt.Sprintf("%d days", days)
		}
	}
	active := strings.Join(q.Types, ",")
	for _, g := range eventGroups {
		v := url.Values{}
		if q.Key != "" {
			v.Set("key", q.Key)
		}
		if len(g.types) > 0 {
			v.Set("type", strings.Join(g.types, ","))
		}
		href := "/history"
		if enc := v.Encode(); enc != "" {
			href += "?" + enc
		}
		p.Filters = append(p.Filters, filter{Label: g.label, Href: href, Active: active == strings.Join(g.types, ",")})
	}
	h.render(w, "history", http.StatusOK, p)
}
