package compose

import "strings"

// RollbackCommands are the two commands that undo a pull (plan §3.7): tag the old image back onto the reference, then
// bring the service up from it without pulling. They are written for a person at the host's shell, so they use the
// host's `docker compose`, not the binary whalenurse itself runs, and carry the project, directory, files and env files
// the container was created with, so they are exact.
func RollbackCommands(p Project, ref, oldImage string) (tag, up string) {
	tag = "docker image tag " + oldImage + " " + shellWord(ref)
	args := append([]string{"docker", "compose"}, p.baseArgs()...)
	args = append(args, "up", "-d", "--no-deps", "--pull", "never", p.Service)
	for i, a := range args {
		args[i] = shellWord(a)
	}
	return tag, strings.Join(args, " ")
}

// shellWord quotes a word for a POSIX shell only when it needs it.
func shellWord(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
