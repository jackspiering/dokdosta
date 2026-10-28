package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestDefaultsMatchThePlan(t *testing.T) {
	g, warns, err := FromEnv([]string{"PATH=/bin", "DOKDOSTA_HOST_ID=box"})
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if g.HostID != "box" || g.Mode != ModeLabel || g.DryRun || g.PollInterval != 60*time.Second || g.Grace != 30*time.Second ||
		g.Cooldown != 30*time.Second || g.CrashloopAfter != 5*time.Minute || g.RateLimit != 10 || g.StartupSettle != 2*time.Minute ||
		g.LogLevel != slog.LevelInfo || g.HTTPAddr != "" ||
		g.RecoveryPeriod != 10*time.Minute || g.FlapLimit != 3 || g.FlapWindow != 24*time.Hour || g.DataDir != "/data" || g.HistoryRetention != 30*24*time.Hour ||
		g.DriftGuard != DriftStrict || g.ComposeBin != "" {
		t.Fatalf("%+v", g)
	}
}

func TestEnvOverridesAndErrors(t *testing.T) {
	g, warns, err := FromEnv([]string{
		"DOKDOSTA_MODE=ALL", "DOKDOSTA_DRY_RUN=true", "DOKDOSTA_GRACE=5s", "DOKDOSTA_RATE_LIMIT=3",
		"DOKDOSTA_LOG_LEVEL=debug", "DOKDOSTA_HTTP_ADDR=:8484", "DOKDOSTA_STARTUP_SETTLE=0s",
		"DOKDOSTA_FLAP_LIMIT=5", "DOKDOSTA_FLAP_WINDOW=2d", "DOKDOSTA_DATA_DIR=/tmp/x", "DOKDOSTA_COMPOSE_BIN=/x", "DOKDOSTA_DRIFT_GUARD=WARN", "DOKDOSTA_NOTIFY_URLS=ntfy://x", "DOKDOSTA_TYPO=1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if g.Mode != ModeAll || !g.DryRun || g.Grace != 5*time.Second || g.RateLimit != 3 || g.LogLevel != slog.LevelDebug ||
		g.HTTPAddr != ":8484" || g.StartupSettle != 0 || g.FlapLimit != 5 || g.FlapWindow != 48*time.Hour || g.DataDir != "/tmp/x" ||
		g.ComposeBin != "/x" || g.DriftGuard != DriftWarn {
		t.Fatalf("%+v", g)
	}
	joined := strings.Join(warns, "|")
	if len(g.NotifyURLs) != 1 || g.NotifyURLs[0] != "ntfy://x" {
		t.Errorf("notify urls: %v", g.NotifyURLs)
	}
	for _, want := range []string{"DOKDOSTA_TYPO is not a known variable"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing warning %q in %v", want, warns)
		}
	}
	// The notify URL may hold a token: it must never be echoed in a warning.
	if strings.Contains(joined, "ntfy://") {
		t.Errorf("a warning leaked a variable value: %v", warns)
	}

	for _, bad := range []string{"DOKDOSTA_MODE=nope", "DOKDOSTA_GRACE=soon", "DOKDOSTA_POLL_INTERVAL=0s", "DOKDOSTA_RATE_LIMIT=0", "DOKDOSTA_DRY_RUN=maybe", "DOKDOSTA_LOG_LEVEL=loud", "DOKDOSTA_FLAP_LIMIT=0", "DOKDOSTA_RECOVERY_PERIOD=0s", "DOKDOSTA_DRIFT_GUARD=lenient"} {
		if _, _, err := FromEnv([]string{bad}); err == nil {
			t.Errorf("%s should be rejected", bad)
		}
	}
}

func TestParseLabels(t *testing.T) {
	cases := []struct {
		name   string
		mode   Mode
		labels map[string]string
		opted  bool
		acts   string
		heal   HealExited
		cd     time.Duration
		warns  int
	}{
		{"label mode, no label", ModeLabel, nil, false, "restart,recreate", HealCrashed, 0, 0},
		{"enabled", ModeLabel, map[string]string{LabelEnable: "true"}, true, "restart,recreate", HealCrashed, 0, 0},
		{"all mode", ModeAll, nil, true, "restart,recreate", HealCrashed, 0, 0},
		{"all mode opt-out", ModeAll, map[string]string{LabelEnable: "false"}, false, "restart,recreate", HealCrashed, 0, 0},
		{"bad enable keeps the mode default", ModeLabel, map[string]string{LabelEnable: "yes please"}, false, "restart,recreate", HealCrashed, 0, 1},
		{"actions", ModeLabel, map[string]string{LabelEnable: "1", LabelActions: " Restart "}, true, "restart", HealCrashed, 0, 0},
		{"bad action falls back", ModeLabel, map[string]string{LabelEnable: "1", LabelActions: "restart,reboot"}, true, "restart,recreate", HealCrashed, 0, 1},
		{"pull only via heal.pull", ModeLabel, map[string]string{LabelEnable: "1", LabelActions: "restart,pull"}, true, "restart,recreate", HealCrashed, 0, 1},
		{"heal.pull appends the rung", ModeLabel, map[string]string{LabelEnable: "1", LabelHealPull: "true"}, true, "restart,recreate,pull", HealCrashed, 0, 0},
		{"heal-exited", ModeLabel, map[string]string{LabelEnable: "1", LabelHealExited: "always"}, true, "restart,recreate", HealAlways, 0, 0},
		{"bad heal-exited", ModeLabel, map[string]string{LabelEnable: "1", LabelHealExited: "sometimes"}, true, "restart,recreate", HealCrashed, 0, 1},
		{"cooldown", ModeLabel, map[string]string{LabelEnable: "1", LabelCooldown: "2m"}, true, "restart,recreate", HealCrashed, 2 * time.Minute, 0},
		{"bad cooldown", ModeLabel, map[string]string{LabelEnable: "1", LabelCooldown: "-1s"}, true, "restart,recreate", HealCrashed, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, warns := ParseLabels(tc.labels, tc.mode)
			if c.OptedIn != tc.opted || strings.Join(c.Actions, ",") != tc.acts || c.HealExited != tc.heal || c.Cooldown != tc.cd || len(warns) != tc.warns {
				t.Fatalf("%+v warns=%v", c, warns)
			}
		})
	}
}

func TestCooldownFor(t *testing.T) {
	base := 30 * time.Second
	for rung, want := range map[int]time.Duration{0: 30 * time.Second, 1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 4: 5 * time.Minute, 9: 5 * time.Minute} {
		if got := CooldownFor(base, 0, rung); got != want {
			t.Errorf("rung %d: %s, want %s", rung, got, want)
		}
	}
	if got := CooldownFor(base, 10*time.Minute, 0); got != CooldownMax {
		t.Errorf("override is capped: %s", got)
	}
	if got := CooldownFor(base, 3*time.Minute, 0); got != 3*time.Minute {
		t.Errorf("override: %s", got)
	}
}

func TestParseDurationAcceptsDays(t *testing.T) {
	for in, want := range map[string]time.Duration{"30d": 30 * 24 * time.Hour, "2d12h": 60 * time.Hour, "90m": 90 * time.Minute, "0": 0} {
		if got, err := ParseDuration(in); err != nil || got != want {
			t.Errorf("%q = %v, %v; want %v", in, got, err, want)
		}
	}
	// 213504d wraps int64 nanoseconds to about 25 minutes; the largest that fits is accepted.
	for _, bad := range []string{"d", "x1d", "1dd", "1d3", "213504d", "99999999999d", "106751d23h60m", "106751d99999h"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestFlapLimitLabel(t *testing.T) {
	c, w := ParseLabels(map[string]string{LabelEnable: "true", LabelFlapLimit: "7"}, ModeLabel)
	if c.FlapLimit != 7 || len(w) != 0 {
		t.Fatalf("%+v %v", c, w)
	}
	if c, w := ParseLabels(map[string]string{LabelEnable: "true", LabelFlapLimit: "0"}, ModeLabel); c.FlapLimit != 0 || len(w) != 1 {
		t.Fatalf("%+v %v", c, w)
	}
}

func TestDisabledLabelIsDistinctFromNotOptedIn(t *testing.T) {
	for _, tc := range []struct {
		mode     Mode
		labels   map[string]string
		opted, d bool
	}{
		{ModeLabel, nil, false, false},
		{ModeLabel, map[string]string{LabelEnable: "true"}, true, false},
		{ModeLabel, map[string]string{LabelEnable: "false"}, false, true},
		{ModeAll, nil, true, false},
		{ModeAll, map[string]string{LabelEnable: "false"}, false, true},
	} {
		c, _ := ParseLabels(tc.labels, tc.mode)
		if c.OptedIn != tc.opted || c.Disabled != tc.d {
			t.Errorf("%v %v: opted=%v disabled=%v", tc.mode, tc.labels, c.OptedIn, c.Disabled)
		}
	}
}

func TestSplitURLs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"ntfy://a", []string{"ntfy://a"}},
		{"ntfy://a,discord://b@c", []string{"ntfy://a", "discord://b@c"}},
		{"ntfy://a, discord://b@c ,\n gotify://h/t", []string{"ntfy://a", "discord://b@c", "gotify://h/t"}},
		{"ntfy://a discord://b@c\ngotify://h/t", []string{"ntfy://a", "discord://b@c", "gotify://h/t"}},
		// A comma inside a URL is part of it.
		{"ntfy://ntfy.sh/topic?tags=warning,skull,discord://b@c", []string{"ntfy://ntfy.sh/topic?tags=warning,skull", "discord://b@c"}},
		{"telegram://tok@telegram?chats=@ops,@dev,generic://h/p?template=json", []string{"telegram://tok@telegram?chats=@ops,@dev", "generic://h/p?template=json"}},
		{"generic+https://h/p,ntfy://x", []string{"generic+https://h/p", "ntfy://x"}},
		{",,ntfy://a,,", []string{"ntfy://a"}},
		// A comma followed by scheme:// splits, even inside a query value (an ntfy action button); %2C keeps it whole.
		{"ntfy://h/t?actions=view,Open,https://x", []string{"ntfy://h/t?actions=view,Open", "https://x"}},
		{"ntfy://h/t?actions=view%2COpen%2Chttps://x", []string{"ntfy://h/t?actions=view%2COpen%2Chttps://x"}},
	}
	for _, tc := range cases {
		got := SplitURLs(tc.in)
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("SplitURLs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNotifyVariables(t *testing.T) {
	g, warns, err := FromEnv([]string{"DOKDOSTA_NOTIFY_URLS=ntfy://tok-secret@h/t", "DOKDOSTA_HEARTBEAT_URL=https://hc-ping.com/uuid-secret",
		"DOKDOSTA_NOTIFY_TIMEOUT=3s", "DOKDOSTA_NOTIFY_RATE_LIMIT=7", "DOKDOSTA_HEARTBEAT_INTERVAL=90s"})
	if err != nil || len(warns) != 0 {
		t.Fatalf("%v %v", err, warns)
	}
	if g.NotifyTimeout != 3*time.Second || g.NotifyRateLimit != 7 || g.PingInterval != 90*time.Second || g.PingURL != "https://hc-ping.com/uuid-secret" {
		t.Fatalf("%+v", g)
	}
	d := Defaults()
	if d.NotifyTimeout != 10*time.Second || d.NotifyRateLimit != 20 || d.PingInterval != 5*time.Minute || len(d.NotifyURLs) != 0 || d.PingURL != "" {
		t.Fatalf("defaults: %+v", d)
	}
	for _, bad := range []string{"DOKDOSTA_NOTIFY_TIMEOUT=0s", "DOKDOSTA_NOTIFY_RATE_LIMIT=0", "DOKDOSTA_HEARTBEAT_INTERVAL=soon"} {
		if _, _, err := FromEnv([]string{bad, "DOKDOSTA_NOTIFY_URLS=ntfy://tok-secret@h/t"}); err == nil {
			t.Errorf("%s should be rejected", bad)
		} else if strings.Contains(err.Error(), "tok-secret") {
			t.Errorf("an error quotes a notification URL: %v", err)
		}
	}
}

// Rung 3 requires rung 2 (plan v3.8): heal.pull=true without recreate in dokdosta.actions gives one warning and the pull
// rung stays off.
func TestHealPullNeedsRecreateInTheLadder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels map[string]string
		acts   string
		warns  int
	}{
		{"default actions", map[string]string{LabelEnable: "1", LabelHealPull: "true"}, "restart,recreate,pull", 0},
		{"restart only", map[string]string{LabelEnable: "1", LabelActions: "restart", LabelHealPull: "true"}, "restart", 1},
		{"recreate only", map[string]string{LabelEnable: "1", LabelActions: "recreate", LabelHealPull: "true"}, "recreate,pull", 0},
		{"reordered", map[string]string{LabelEnable: "1", LabelActions: "recreate,restart", LabelHealPull: "true"}, "recreate,restart,pull", 0},
		{"pull false, restart only", map[string]string{LabelEnable: "1", LabelActions: "restart", LabelHealPull: "false"}, "restart", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, warns := ParseLabels(tc.labels, ModeLabel)
			if got := strings.Join(c.Actions, ","); got != tc.acts {
				t.Errorf("actions = %s, want %s", got, tc.acts)
			}
			if len(warns) != tc.warns {
				t.Errorf("warnings = %v, want %d", warns, tc.warns)
			}
			if tc.warns == 1 && !strings.Contains(warns[0], "needs recreate") {
				t.Errorf("%v", warns)
			}
		})
	}
}

func TestHTTPTokenAndMetrics(t *testing.T) {
	g, warns, err := FromEnv([]string{"DOKDOSTA_HTTP_TOKEN=tok-secret-value", "DOKDOSTA_METRICS=true", "DOKDOSTA_HTTP_ADDR=127.0.0.1:8484"})
	if err != nil || g.HTTPToken != "tok-secret-value" || !g.Metrics || g.HTTPAddr != "127.0.0.1:8484" {
		t.Fatalf("%+v %v", g, err)
	}
	for _, w := range warns {
		t.Errorf("warning: %s", w)
	}
	g, _, _ = FromEnv(nil)
	if g.HTTPToken != "" || g.Metrics || g.HTTPAddr != "" {
		t.Fatalf("off by default: %+v", g)
	}
	// An invalid value is an error that does not quote a token; the token itself is never in an error.
	_, _, err = FromEnv([]string{"DOKDOSTA_HTTP_TOKEN=tok-secret-value", "DOKDOSTA_METRICS=maybe"})
	if err == nil || strings.Contains(err.Error(), "tok-secret-value") {
		t.Fatalf("%v", err)
	}
}

func TestHTTPAllowedHosts(t *testing.T) {
	g, warns, err := FromEnv([]string{"DOKDOSTA_HTTP_ALLOWED_HOSTS=nas.example.org, 192.168.1.5\tmedia.lan"})
	if err != nil || len(warns) != 0 || strings.Join(g.HTTPAllowedHosts, "|") != "nas.example.org|192.168.1.5|media.lan" {
		t.Fatalf("%v %v %v", g.HTTPAllowedHosts, warns, err)
	}
	if g, _, _ = FromEnv(nil); len(g.HTTPAllowedHosts) != 0 {
		t.Fatal("empty by default")
	}
}

func TestHTTPTrustedProxies(t *testing.T) {
	g, warns, err := FromEnv([]string{"DOKDOSTA_HTTP_TRUSTED_PROXIES=172.18.0.0/16, 10.0.0.5\tfd00::/8 192.168.1.77/24 ::ffff:10.1.2.0/120"})
	if err != nil || len(warns) != 0 {
		t.Fatalf("%v %v", warns, err)
	}
	var got []string
	for _, p := range g.HTTPTrustedProxies {
		got = append(got, p.String())
	}
	// A single address is a host prefix; host bits of a CIDR are masked; an IPv4-mapped prefix becomes the IPv4 range.
	if want := "172.18.0.0/16 10.0.0.5/32 fd00::/8 192.168.1.0/24 10.1.2.0/24"; strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
	if g, _, _ = FromEnv(nil); len(g.HTTPTrustedProxies) != 0 {
		t.Fatal("empty by default")
	}
	for _, bad := range []string{"nginx", "10.0.0.0/33", "0.0.0.0/0", "::/0", "10.0.0.0/8 proxy.example", "::ffff:0.0.0.0/95"} {
		if _, _, err := FromEnv([]string{"DOKDOSTA_HTTP_TRUSTED_PROXIES=" + bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	// Whoever sets this also sets a token; an error about the list never mentions the token.
	_, _, err = FromEnv([]string{"DOKDOSTA_HTTP_TOKEN=tok-secret-value", "DOKDOSTA_HTTP_TRUSTED_PROXIES=0.0.0.0/0"})
	if err == nil || strings.Contains(err.Error(), "tok-secret-value") || !strings.Contains(err.Error(), "trusts every address") {
		t.Fatalf("%v", err)
	}
}

func TestRegistryAuthDirIsRead(t *testing.T) {
	g, warns, err := FromEnv([]string{"DOKDOSTA_REGISTRY_AUTH_DIR=/registry-auth"})
	if err != nil || len(warns) != 0 || g.RegistryAuthDir != "/registry-auth" {
		t.Fatalf("%q %v %v", g.RegistryAuthDir, warns, err)
	}
	g, _, err = FromEnv([]string{"DOKDOSTA_REGISTRY_AUTH_DIR=  "})
	if err != nil || g.RegistryAuthDir != "" {
		t.Fatalf("%q %v", g.RegistryAuthDir, err)
	}
}

func TestParseDurationAcceptsTheLargestWholeDays(t *testing.T) {
	if got, err := ParseDuration("106751d"); err != nil || got != 106751*24*time.Hour {
		t.Errorf("106751d = %v, %v", got, err)
	}
}

func TestHostIDWithASlashIsRefused(t *testing.T) {
	_, _, err := FromEnv([]string{"DOKDOSTA_HOST_ID=eu/box"})
	if err == nil || !strings.Contains(err.Error(), "DOKDOSTA_HOST_ID") {
		t.Fatalf("a slash in the host id should be an error, got %v", err)
	}
	if g, _, err := FromEnv([]string{"DOKDOSTA_HOST_ID=eu-box.1"}); err != nil || g.HostID != "eu-box.1" {
		t.Fatalf("%+v %v", g, err)
	}
}

func TestPlaceholderTokenOfTheExamplesIsRefused(t *testing.T) {
	for _, v := range []string{"replace-with-a-long-random-token", "replace-with-openssl-rand-hex-24", "Replace-With-Mine"} {
		_, _, err := FromEnv([]string{"DOKDOSTA_HTTP_TOKEN=" + v})
		if err == nil || !strings.Contains(err.Error(), "DOKDOSTA_HTTP_TOKEN") || strings.Contains(err.Error(), v) {
			t.Errorf("%q: want an error that names the variable and not the value, got %v", v, err)
		}
	}
	if g, _, err := FromEnv([]string{"DOKDOSTA_HTTP_TOKEN=0123456789abcdef0123456789abcdef"}); err != nil || g.HTTPToken == "" {
		t.Fatalf("%v", err)
	}
}

// Plan v3.15 item 4: a variable from before the rename is a startup error that names it and never its value.
func TestVariablesFromBeforeTheRenameAreRefusedByNameOnly(t *testing.T) {
	_, _, err := FromEnv([]string{"WHALENURSE_NOTIFY_URLS=ntfy://secret-topic", "DOKDOSTA_MODE=all", "WHALENURSE_HTTP_TOKEN=s3cr3t-token-value", "WHALENURSE_EMPTY="})
	if err == nil {
		t.Fatal("WHALENURSE_* accepted")
	}
	msg := err.Error()
	if !strings.Contains(msg, "WHALENURSE_EMPTY, WHALENURSE_HTTP_TOKEN, WHALENURSE_NOTIFY_URLS set") || !strings.Contains(msg, "renamed dokdosta") {
		t.Errorf("error does not name the variables, sorted: %s", msg)
	}
	for _, v := range []string{"secret-topic", "s3cr3t-token-value", "ntfy"} {
		if strings.Contains(msg, v) {
			t.Errorf("error quotes a value (%q): %s", v, msg)
		}
	}
	if _, _, err := FromEnv([]string{"DOKDOSTA_MODE=all", "XWHALENURSE_A=1", "whalenurse_a=1"}); err != nil {
		t.Errorf("only the WHALENURSE_ prefix is refused: %v", err)
	}
}

// Plan v3.15 item 5: a label from before the rename leaves the container alone, whatever else it carries, and the
// warning names the keys, never a value.
func TestLabelsFromBeforeTheRenameLeaveTheContainerAlone(t *testing.T) {
	c, warns := ParseLabels(map[string]string{"whalenurse.heal.pull": "secret-value", "whalenurse.enable": "true", LabelEnable: "true"}, ModeAll)
	if c.OptedIn || !c.Disabled || strings.Join(c.Legacy, ",") != "whalenurse.enable,whalenurse.heal.pull" {
		t.Fatalf("%+v", c)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "(whalenurse.enable, whalenurse.heal.pull)") || strings.Contains(warns[0], "secret-value") {
		t.Fatalf("%q", warns)
	}
	if c, _ := ParseLabels(map[string]string{LabelEnable: "true", "xwhalenurse.enable": "false"}, ModeLabel); !c.OptedIn || c.Legacy != nil {
		t.Fatalf("only the whalenurse. prefix counts: %+v", c)
	}
}
