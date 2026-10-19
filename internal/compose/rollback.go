package compose

import "strings"

// RollbackCommands are the two commands that undo a pull (plan §3.7): tag the old image back onto the reference, then
// bring the service up from it without pulling. They are written for a person at the host's shell, so they use the
// host's `docker compose`, not the binary whalenurse itself runs, and carry the project, directory, files and env files
// the container was created with, so they are exact.
//
// alsoServices are sidecars of the same project that run the same image and were recreated with it (plan v3.8): the one
// `up` names them after the service, so a single paste puts them all back.
func RollbackCommands(p Project, ref, oldImage string, alsoServices ...string) (tag, up string) {
	tag = "docker image tag " + oldImage + " " + shellWord(ref)
	args := append([]string{"docker", "compose"}, p.baseArgs()...)
	args = append(args, "up", "-d", "--no-deps", "--pull", "never", p.Service)
	args = append(args, alsoServices...)
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
