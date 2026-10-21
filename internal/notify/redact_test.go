package notify

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// Tokens, as they sit in real URLs: user info, a path, a query value, a host (Discord's webhook id).
var secretURLs = map[string][]string{
	"discord://hunter2tokenAAA@123456789012345678":                                                 {"hunter2tokenAAA", "123456789012345678"},
	"telegram://9876543210:AAEhBP0-secret_bot_token@telegram?chats=@ops,@dev":                      {"AAEhBP0-secret_bot_token", "9876543210"},
	"ntfy://user:p4ssw0rd-xyz@ntfy.example.com/topic-secret?tags=warning,skull":                    {"p4ssw0rd-xyz", "topic-secret"},
	"gotify://gotify.example.com/path/ApP-tOkEn-123":                                               {"ApP-tOkEn-123"},
	"generic://hooks.example.com/services/T000/B000/XXXXsecretXXXX?template=json&$key=q-value-777": {"XXXXsecretXXXX", "q-value-777"},
	"pushover://shoutrrr:apiToken-abcdef@userKey-ghijkl/":                                          {"apiToken-abcdef", "userKey-ghijkl"},
}

func TestRedactorRemovesEveryPartOfTheURL(t *testing.T) {
	var all []string
	for u := range secretURLs {
		all = append(all, u)
	}
	red := NewRedactor(all...)
	for u, secrets := range secretURLs {
		parsed, _ := url.Parse(u)
		// What a failing HTTP client, or Shoutrrr, might say about this URL.
		errs := []error{
			fmt.Errorf("Post %q: dial tcp: connection refused", u),
			fmt.Errorf("Post %q: connection refused", "https://"+strings.TrimPrefix(u, parsed.Scheme+"://")),
			fmt.Errorf("sending message: HTTP 401 from %s", parsed.Host),
			&url.Error{Op: "Post", URL: "https://" + parsed.Host + parsed.EscapedPath() + "?" + parsed.RawQuery, Err: errors.New("timeout")},
			fmt.Errorf("bad token %s in %s", url.QueryEscape(secrets[0]), url.PathEscape(secrets[0])),
		}
		for _, err := range errs {
			got := red.Err(err)
			for _, s := range secrets {
				if strings.Contains(got, s) {
					t.Errorf("%s: %q still holds %q", u, got, s)
				}
			}
			if !strings.Contains(got, mask) {
				t.Errorf("%s: nothing was masked in %q", u, got)
			}
		}
	}
}

func TestRedactorKeepsOrdinaryText(t *testing.T) {
	red := NewRedactor("ntfy://user:p4ssw0rd-xyz@ntfy.example.com/topic-secret?tags=a,b&x=yes")
	for _, s := range []string{"connection refused", "HTTP 500", "context deadline exceeded", "yes it is a tag"} {
		if got := red.Redact(s); got != s {
			t.Errorf("%q became %q: short fragments must stay", s, got)
		}
	}
	var nilRed *Redactor
	if nilRed.Redact("x") != "x" || nilRed.Err(nil) != "" {
		t.Error("a nil redactor must pass text through")
	}
}

func TestRedactorHandlesAURLThatDoesNotParse(t *testing.T) {
	raw := "ntfy://user:p%zz@bad host/secret-topic-path"
	red := NewRedactor(raw)
	got := red.Redact("failed: " + raw + " (secret-topic-path)")
	if strings.Contains(got, "secret-topic-path") || strings.Contains(got, "user:p") {
		t.Errorf("leaked: %q", got)
	}
}

func TestLabelNamesTheServiceAndNothingElse(t *testing.T) {
	cases := map[string]string{
		"discord://tok@123":              "#1 discord",
		"generic+https://h.example/path": "#1 generic+https",
		"NTFY://x":                       "#1 ntfy",
		"not a url with token-abc":       "#1 invalid",
		"://x":                           "#1 invalid",
	}
	for raw, want := range cases {
		if got := Label(0, raw); got != want {
			t.Errorf("Label(%q) = %q, want %q", raw, got, want)
		}
	}
	if Label(2, "ntfy://x") != "#3 ntfy" {
		t.Error("labels are 1-based positions")
	}
}

func TestRedactorLeavesShortPathWordsAlone(t *testing.T) {
	red := NewRedactor("generic://127.0.0.1:8080/hook?template=json")
	got := red.Redact("failed to send notification to generic webhook: Post http://127.0.0.1:8080/hook")
	if !strings.Contains(got, "generic webhook") || strings.Contains(got, "127.0.0.1") {
		t.Errorf("%q", got)
	}
}

// The registry credentials of the pull rung are literal secrets, not URLs: each goes whole, and escaped.
func TestRedactorRemovesRegistryCredentials(t *testing.T) {
	r := NewRedactorWith([]string{"YWxpY2U6cDQ1NXcwcmQ=", "alice:p455w0rd", "p455 w0rd/x", "ab"}, "ntfy://ntfy.example.com/topic-SECRET-1")
	in := "unauthorized: auth YWxpY2U6cDQ1NXcwcmQ= user:pass alice:p455w0rd q=p455+w0rd%2Fx p=p455%20w0rd%2Fx on topic-SECRET-1 ab"
	out := r.Redact(in)
	for _, s := range []string{"YWxpY2U6cDQ1NXcwcmQ=", "alice:p455w0rd", "p455+w0rd%2Fx", "p455%20w0rd%2Fx", "topic-SECRET-1"} {
		if strings.Contains(out, s) {
			t.Errorf("%q survived: %s", s, out)
		}
	}
	if !strings.HasSuffix(out, " ab") || !strings.HasPrefix(out, "unauthorized:") {
		t.Errorf("ordinary text was mangled: %s", out)
	}
}

// A registry user name is usually the image namespace (ghcr.io/<user>/app): it is not a secret, and masking it would break
// the image references and the pasteable rollback commands that whalenurse shows.
func TestRedactorLeavesImageReferencesOfTheRegistryUserAlone(t *testing.T) {
	r := NewRedactorWith([]string{"YWxpY2U6cDQ1NXcwcmQ=", "p455w0rd"}, "ntfy://ntfy.example.com/topic-SECRET-1")
	in := "docker image tag sha256:0123456789ab ghcr.io/jackspiering/app:1 && docker compose -p stack up -d --no-deps --pull never app"
	if got := r.Redact(in); got != in {
		t.Errorf("an image reference was changed:\n%s\n%s", in, got)
	}
}
