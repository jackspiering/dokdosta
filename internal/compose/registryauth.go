package compose

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// RegistryAuthFile is the file DOKDOSTA_REGISTRY_AUTH_DIR must hold.
const RegistryAuthFile = "config.json"

// RegistryAuth is what dokdosta knows about the credentials the pull rung may use (plan v3.12 items 3 to 5). The
// directory is given to `compose pull` as DOCKER_CONFIG and to no other command, so the file must say nothing but
// credentials: a currentContext would point the pull at another daemon, a credsStore or credHelpers needs a helper
// binary the image does not have, proxies would be sent to every build.
type RegistryAuth struct {
	Dir      string
	Hosts    []string // the keys of "auths", sorted; shown by status, the API and the home page
	Secrets  []string // everything that must never reach a log: the redactor learns these
	Warnings []string // startup warnings: the directory or the file can be written by dokdosta
}

// authEntry is the part of an "auths" entry that dokdosta reads. Docker accepts more fields (email, serveraddress,
// registrytoken); they are not credentials of the kinds dokdosta supports and are ignored.
type authEntry struct {
	Auth          string `json:"auth"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	IdentityToken string `json:"identitytoken"`
}

// LoadRegistryAuth reads and checks the directory. It is called at startup (a failure stops `run`) and again before
// every pull, because compose reads the file itself on each pull. No error quotes a value from the file: a key of the
// top level is named, a credential never is.
func LoadRegistryAuth(dir string) (*RegistryAuth, error) {
	if dir == "" {
		return nil, errors.New("no directory")
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, pathErr(err))
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory (it must hold %s)", dir, RegistryAuthFile)
	}
	if _, err := os.Lstat(filepath.Join(dir, "contexts")); err == nil {
		return nil, fmt.Errorf("%s holds a contexts entry: a Docker context could point the pull at another daemon, so it is refused", dir)
	}
	path := filepath.Join(dir, RegistryAuthFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, pathErr(err))
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("%s is not a JSON object", path) // json's own message can quote a byte of the file
	}
	var keys []string
	for k := range top {
		if k != "auths" {
			keys = append(keys, k)
		}
	}
	if len(keys) > 0 {
		sort.Strings(keys)
		return nil, fmt.Errorf("%s: the key %s is not allowed, the file may hold only \"auths\" with inline credentials (no credsStore, credHelpers, currentContext, proxies or anything else)", path, quoteKey(keys[0]))
	}
	var auths map[string]authEntry
	if raw, ok := top["auths"]; ok {
		err = json.Unmarshal(raw, &auths)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: \"auths\" is not an object of registry entries", path)
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("%s: \"auths\" is empty or missing", path)
	}
	a := &RegistryAuth{Dir: dir}
	secrets := map[string]bool{}
	secret := func(s string) {
		if s != "" {
			secrets[s] = true
		}
	}
	for host, e := range auths {
		if host == "" {
			return nil, fmt.Errorf("%s: an \"auths\" entry has an empty registry name", path)
		}
		if e.Auth != "" {
			user, pass, err := decodeAuth(e.Auth)
			if err != nil {
				return nil, fmt.Errorf("%s: the \"auth\" of %s %v", path, quoteKey(host), err)
			}
			// The user name is not a secret and is not redacted: on GHCR and Docker Hub it is usually the image namespace
			// (ghcr.io/<user>/app), and masking it would break every image reference and rollback command shown.
			secret(e.Auth)
			secret(user + ":" + pass)
			secret(pass)
		} else if e.Username != "" && e.Password != "" {
			// the password is collected below
		} else if e.IdentityToken == "" {
			return nil, fmt.Errorf("%s: the entry of %s has no inline credential (want \"auth\", \"username\" with \"password\", or \"identitytoken\")", path, quoteKey(host))
		}
		secret(e.Password)
		secret(e.IdentityToken)
		a.Hosts = append(a.Hosts, host)
	}
	sort.Strings(a.Hosts)
	for s := range secrets {
		a.Secrets = append(a.Secrets, s)
	}
	sort.Strings(a.Secrets)
	for _, p := range []string{dir, path} {
		if syscall.Access(p, 2) == nil { // W_OK
			a.Warnings = append(a.Warnings, fmt.Sprintf("%s can be written by dokdosta: mount the credentials read-only", p))
		}
	}
	return a, nil
}

// decodeAuth reads Docker's "auth" field: base64 of user:password.
func decodeAuth(s string) (user, pass string, err error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		if b, err = base64.RawStdEncoding.DecodeString(s); err != nil {
			return "", "", errors.New("is not base64")
		}
	}
	u, p, found := strings.Cut(string(b), ":")
	if !found || u == "" || p == "" {
		return "", "", errors.New("does not decode to user:password")
	}
	return u, p, nil
}

// quoteKey shows a key of the file: it names a registry or a forbidden setting, it is not a credential, but a typo could
// put a value there, so it is cut.
func quoteKey(k string) string {
	if len(k) > 40 {
		k = k[:40] + "..."
	}
	return fmt.Sprintf("%q", k)
}

// pathErr drops the path from an os error (the caller names it) and keeps the cause.
func pathErr(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
