package compose

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Source says where the compose binary came from (plan §3.4). The distinction is made here, by the path that matched,
// and never by comparing versions: the version-skew check of the drift guard applies to the bundled copy only.
type Source string

const (
	SourceEnv     Source = "env"     // DOKDOSTA_COMPOSE_BIN
	SourceHost    Source = "host"    // the host's plugin: mounted read-only at HostMountPath, or found in a plugin directory
	SourceBundled Source = "bundled" // the copy in the image
)

// Fixed paths (plan §3.4). The host plugin and the bundled copy are mounted at different paths on purpose.
const (
	HostMountPath = "/host/docker-compose"
	BundledPath   = "/usr/libexec/dokdosta/docker-compose"
)

// HostPluginDirs are where a host install keeps the compose plugin; they are searched last, for dokdosta under systemd
// (plan §3.4: "In systemd mode, use the host's docker compose"). The order is the Docker CLI's, so dokdosta runs the
// same compose as `docker compose` when two directories hold one.
var HostPluginDirs = []string{
	"/usr/local/lib/docker/cli-plugins/docker-compose",
	"/usr/local/libexec/docker/cli-plugins/docker-compose",
	"/usr/lib/docker/cli-plugins/docker-compose",
	"/usr/libexec/docker/cli-plugins/docker-compose",
}

// Binary is the compose binary in use.
type Binary struct {
	Path    string  `json:"path"`
	Source  Source  `json:"source"`
	Version Version `json:"-"`
	Raw     string  `json:"raw_version,omitempty"` // what `version --short` printed
}

// VersionString is the version for status and logs.
func (b Binary) VersionString() string {
	if b.Version.IsZero() {
		return "unknown"
	}
	return b.Version.String()
}

// Lookup is the search order of plan §3.4. Zero fields take the defaults.
type Lookup struct {
	Env        string   // DOKDOSTA_COMPOSE_BIN
	HostMount  string   // default HostMountPath
	Bundled    string   // default BundledPath
	PluginDirs []string // default HostPluginDirs
	Runner     Runner   // default ExecRunner
	Stat       func(string) (os.FileInfo, error)
}

// Resolve returns the first binary that exists and answers `version --short`: DOKDOSTA_COMPOSE_BIN, then the host
// plugin mounted at HostMountPath, then the bundled copy, then the host's own plugin directories. An explicit
// DOKDOSTA_COMPOSE_BIN that does not work is an error, not a reason to use another binary than the one asked for.
// The returned notes say what was tried, for the startup log.
func Resolve(ctx context.Context, l Lookup) (*Binary, []string, error) {
	if l.HostMount == "" {
		l.HostMount = HostMountPath
	}
	if l.Bundled == "" {
		l.Bundled = BundledPath
	}
	if l.PluginDirs == nil {
		l.PluginDirs = HostPluginDirs
	}
	if l.Runner == nil {
		l.Runner = ExecRunner{}
	}
	if l.Stat == nil {
		l.Stat = os.Stat
	}
	var notes []string
	try := func(path string, src Source) (*Binary, bool) {
		fi, err := l.Stat(path)
		if err != nil || fi.IsDir() {
			notes = append(notes, fmt.Sprintf("%s (%s): not there", path, src))
			return nil, false
		}
		b, err := probe(ctx, l.Runner, path, src)
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s (%s): %v", path, src, err))
			return nil, false
		}
		return b, true
	}
	if l.Env != "" {
		if b, ok := try(l.Env, SourceEnv); ok {
			return b, notes, nil
		}
		return nil, notes, fmt.Errorf("DOKDOSTA_COMPOSE_BIN=%s does not work: %s", l.Env, strings.Join(notes, "; "))
	}
	candidates := []struct {
		path string
		src  Source
	}{{l.HostMount, SourceHost}, {l.Bundled, SourceBundled}}
	for _, d := range l.PluginDirs {
		candidates = append(candidates, struct {
			path string
			src  Source
		}{d, SourceHost})
	}
	for _, c := range candidates {
		if b, ok := try(c.path, c.src); ok {
			return b, notes, nil
		}
	}
	return nil, notes, fmt.Errorf("no docker compose binary found (%s)", strings.Join(notes, "; "))
}

func probe(ctx context.Context, r Runner, path string, src Source) (*Binary, error) {
	res, err := r.Run(ctx, Command{Bin: path, Args: []string{"version", "--short"}, Dir: "/"})
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(res.Stdout))
	b := &Binary{Path: path, Source: src, Raw: raw}
	b.Version, _ = ParseVersion(raw)
	return b, nil
}
