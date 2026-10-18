package compose

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Compose labels that hold what the original `up` was run with (plan §3.4).
const (
	LabelProject     = "com.docker.compose.project"
	LabelService     = "com.docker.compose.service"
	LabelWorkingDir  = "com.docker.compose.project.working_dir"
	LabelConfigFiles = "com.docker.compose.project.config_files"
	LabelEnvFile     = "com.docker.compose.project.environment_file"
	LabelConfigHash  = "com.docker.compose.config-hash"
	LabelImage       = "com.docker.compose.image"
	LabelVersion     = "com.docker.compose.version"
	LabelDependsOn   = "com.docker.compose.depends_on"
)

// Project is what compose needs to act on one service: the project, the directory and the files recorded on the
// container by the `up` that created it.
type Project struct {
	Name        string
	WorkingDir  string
	ConfigFiles []string
	EnvFiles    []string
	Service     string
}

// ProjectFromLabels reads a container's compose labels. It fails when the project, service, working directory or config
// files are missing, which is the "labels are missing" case of plan §3.4: rung 2 is then skipped.
func ProjectFromLabels(labels map[string]string) (Project, error) {
	p := Project{
		Name:        labels[LabelProject],
		Service:     labels[LabelService],
		WorkingDir:  labels[LabelWorkingDir],
		ConfigFiles: splitList(labels[LabelConfigFiles]),
		EnvFiles:    splitList(labels[LabelEnvFile]),
	}
	var missing []string
	if p.Name == "" {
		missing = append(missing, LabelProject)
	}
	if p.Service == "" {
		missing = append(missing, LabelService)
	}
	if p.WorkingDir == "" {
		missing = append(missing, LabelWorkingDir)
	}
	if len(p.ConfigFiles) == 0 {
		missing = append(missing, LabelConfigFiles)
	}
	if len(missing) > 0 {
		return Project{}, fmt.Errorf("not a compose container whalenurse can recreate: label(s) %s missing", strings.Join(missing, ", "))
	}
	return p, nil
}

func splitList(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// Readable checks that the working directory and every file are there to be read. The usual cause of a failure is the
// stacks root not mounted into whalenurse's container at the identical path (plan §3.4, *Mounts*), so the error says so.
func (p Project) Readable(stat func(string) (os.FileInfo, error)) error {
	if stat == nil {
		stat = os.Stat
	}
	var bad []string
	check := func(kind, path string, dir bool) {
		fi, err := stat(path)
		switch {
		case err != nil && errors.Is(err, os.ErrNotExist):
			bad = append(bad, fmt.Sprintf("%s %s does not exist", kind, path))
		case err != nil:
			bad = append(bad, fmt.Sprintf("%s %s: %v", kind, path, err))
		case dir && !fi.IsDir():
			bad = append(bad, fmt.Sprintf("%s %s is not a directory", kind, path))
		}
	}
	check("working directory", p.WorkingDir, true)
	for _, f := range p.ConfigFiles {
		check("compose file", f, false)
	}
	for _, f := range p.EnvFiles {
		check("env file", f, false)
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("compose inputs of project %s are not readable from here: %s; in a container, mount the stacks root at the identical host path (read-only is enough), e.g. /opt/stacks:/opt/stacks:ro",
		p.Name, strings.Join(bad, "; "))
}

// baseArgs is the part of every command line that names the project: -p, --project-directory, -f and --env-file.
func (p Project) baseArgs() []string {
	args := []string{"-p", p.Name, "--project-directory", p.WorkingDir}
	for _, f := range p.ConfigFiles {
		args = append(args, "-f", f)
	}
	for _, f := range p.EnvFiles {
		args = append(args, "--env-file", f)
	}
	return args
}
