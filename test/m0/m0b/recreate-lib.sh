#!/usr/bin/env bash
# The whalenurse-recreate scenario (PLAN v3.6, M2b): whalenurse running for real (not dry-run), in a container built from this
# branch, with the host's compose plugin mounted read-only at /host/docker-compose and the stack directory mounted read-only at
# the identical path. The step breaks the parent in a way only a recreate repairs (a file in its writable layer); whalenurse
# restarts it (no help), recreates it from the stack's own files, and then recreates the network_mode sidecar that was left
# pointing at the old parent. collect.sh requires that the sidecar ends up recreated, in the new parent's namespace, and working.
# Source this file after lib.sh and nurse-lib.sh; do not run it. See stacks/recreate.
#
# Safety: whalenurse acts on containers that carry whalenurse.enable=true, so prepare.sh refuses to continue if any container
# outside whalenurse-m0-b-* carries it. The only project is whalenurse-m0-b-recreate. No daemon restart, no reboot, no sudo.
# shellcheck shell=bash

RECREATE_PROJECT=whalenurse-m0-b-recreate
RECREATE_STACK_BASE=/tmp/whalenurse-m0-b-recreate # world-traversable, unlike captures/ (umask 077): the nonroot user in the image reads the stack here

recreate_key() { nurse_key "$RECREATE_PROJECT" "$1"; }

# recreate_reaches <container>: can it fetch the parent's page on its own loopback?
recreate_reaches() { dk_ctr exec "$1" wget -q -O /dev/null http://127.0.0.1:8080/ >/dev/null 2>&1; }

# recreate_actions <logfile> <key> -> "restart,recreate": the actions whalenurse started on a key, in order.
recreate_actions() {
  jq -r -s --arg k "$2" '[.[] | select(.type == "ActionStarted" and .key == $k) | .["attr.action"]] | join(",")' "$1"
}

recreate_prepare() {
  local sc=whalenurse-recreate P="$RECREATE_PROJECT" gid stack plugin wn i engine compose_v
  m0b_need jq docker awk go git
  m0_sanitize_env
  m0b_require_project "$P"
  [[ -z "$(docker ps -aq --filter "label=com.docker.compose.project=$P")" ]] ||
    m0_die "project $P already exists. Run test/m0/m0b/teardown.sh first (this script never removes it unasked)"
  nurse_preflight_strangers
  m0b_ensure_images

  # The rungs need Engine >= 28.1 and compose >= 5.2.0 (plan section 3.6); on anything older this scenario would only show the gate.
  engine=$(docker version --format '{{.Server.Version}}')
  compose_v=$(docker compose version --short)
  printf 'Engine %s, compose %s\n' "$engine" "$compose_v"
  awk -v e="$engine" -v c="${compose_v#v}" 'BEGIN {
      split(e, a, "."); split(c, b, ".")
      if (a[1] + 0 < 28 || (a[1] + 0 == 28 && a[2] + 0 < 1) || b[1] + 0 < 5 || (b[1] + 0 == 5 && b[2] + 0 < 2)) exit 1 }' ||
    m0_die "Engine $engine / compose $compose_v is below the minimum for rung 2 (Engine 28.1, compose 5.2.0)"

  m0b_state_new "$sc"
  m0b_set t_prepare "$EPOCHREALTIME"
  m0b_set project "$P"
  m0b_versions "$STATE/versions-before.txt"
  nurse_build_image

  gid=$(stat -c %g /var/run/docker.sock)
  plugin=$(compose_plugin_path)
  [[ -x "$plugin" ]] || m0_die "cannot find the host compose plugin ($plugin)"
  stack="$RECREATE_STACK_BASE/$(basename "$STATE")"
  [[ "$stack" == /tmp/whalenurse-m0-b-recreate/* ]] || m0_die "unexpected stack directory $stack"
  mkdir -p "$stack"
  chmod 755 "$RECREATE_STACK_BASE" "$stack"
  cp "$M0B_HERE/stacks/recreate/compose.yaml" "$stack/compose.yaml"
  printf 'NURSE_IMAGE=%s\nDOCKER_GID=%s\nCOMPOSE_PLUGIN=%s\nSTACK_DIR=%s\n' "$NURSE_IMAGE" "$gid" "$plugin" "$stack" >"$stack/vars.env"
  chmod 644 "$stack/compose.yaml" "$stack/vars.env"
  m0b_set stack_dir "$stack"
  m0b_set host_plugin "$plugin"

  echo "== starting whalenurse, the parent and the sidecar from $stack"
  dcs "$P" "$stack" up -d --pull never --no-build >"$STATE/up.txt" 2>&1 || {
    tail -5 "$STATE/up.txt"
    m0_die "compose up failed (see $STATE/up.txt)"
  }
  wn=$(cid "$P" whalenurse)
  wait_for 90 is_running "$wn" || m0_die "whalenurse is not running: docker logs $wn"
  wait_for 90 is_healthy "$(cid "$P" parent)" || m0_die "the parent is not healthy"
  wait_for 90 is_healthy "$(cid "$P" sidecar)" || m0_die "the sidecar is not healthy"
  echo "== waiting for whalenurse to finish its startup settle"
  for ((i = 0; i < 60; i++)); do
    if nurse_status "$wn" "$STATE/baseline-status.json" && [[ "$(jq -r '.settled' "$STATE/baseline-status.json")" == true ]]; then break; fi
    sleep 2
  done
  [[ "$(jq -r '.settled // false' "$STATE/baseline-status.json" 2>/dev/null)" == true ]] || {
    docker logs --tail 30 "$wn" 2>&1
    m0_die "whalenurse did not settle"
  }
  [[ "$(jq -r '.compose.source' "$STATE/baseline-status.json")" == host && "$(jq -r '.compose.recreate_enabled' "$STATE/baseline-status.json")" == true ]] ||
    m0_die "whalenurse is not using the host compose with the rungs enabled: $(jq -c .compose "$STATE/baseline-status.json")"
  recreate_reaches "$(cid "$P" sidecar)" || m0_die "the sidecar cannot reach the parent before the step"

  mkdir -p "$STATE/baseline"
  m0b_table "$P" >"$STATE/baseline/table.tsv"
  m0b_netns "$P" >"$STATE/baseline/netns.tsv"
  m0b_inspect_dir "$P" "$STATE/baseline/inspect"
  nurse_logs "$wn" 0 "$STATE/baseline/whalenurse-log.ndjson"
  # Everything else on the host, to prove it is untouched.
  docker ps -aq --no-trunc --format '{{.ID}} {{.Label "com.docker.compose.project"}}' | awk -v p="$P" '$2 != p {print $1}' | sort >"$STATE/baseline/others.txt"
  for s in whalenurse parent sidecar; do m0b_set "baseline_$s" "$(cid "$P" "$s")"; done
  m0b_set nurse_container "$wn"
  m0b_set nurse_started "$(docker inspect --format '{{.State.StartedAt}}' "$wn")"
  {
    echo "baseline at $(date -u +%FT%TZ), image $NURSE_IMAGE, compose $compose_v from $plugin, Engine $engine"
    column -t -s$'\t' "$STATE/baseline/table.tsv"
    column -t -s$'\t' "$STATE/baseline/netns.tsv"
  } >"$STATE/baseline/summary.txt"
  cat "$STATE/baseline/summary.txt"

  m0b_write_step "break the parent with a file in its writable layer: whalenurse restarts it (no help), recreates it from the stack's files (help), then recreates the sidecar" \
    "docker exec $(cid "$P" parent) touch /broken"
  printf '%s\n' \
    'Notes:' \
    "  - Everything in $P stays in place until test/m0/m0b/teardown.sh (which also removes $stack)." \
    '  - No daemon restart and no sudo. The step is one docker exec into the parent of this scenario.' \
    '  - collect.sh waits (up to --watch seconds, default 240) for the parent and the sidecar to be recreated and healthy,' \
    '    then checks that the sidecar shares the new parent namespace and reaches it, and that whalenurse never restarted it.' \
    ''
  m0b_print_collect "$sc" "--watch 240"
}

# recreate_collect [--watch SECS] [state dir]
recreate_collect() {
  local sc=whalenurse-recreate watch=240 dir="" P wn out sum t_step i k_parent k_sidecar p_id s_id acts fails=0 warns=0 ns
  while (($#)); do
    case "$1" in
      --watch)
        watch="${2:?--watch SECS}"
        shift 2
        ;;
      *)
        dir="$1"
        shift
        ;;
    esac
  done
  m0b_need jq docker awk
  m0_sanitize_env
  m0b_state_find "$sc" "$dir"
  # shellcheck disable=SC2154  # these come from state.env
  P="$project"
  m0b_require_project "$P"
  out="$STATE/collected"
  mkdir -p "$out"
  sum="$out/summary.txt"
  : >"$sum"
  csay() { printf '%s\n' "$*" | tee -a "$sum"; }
  pass() { csay "PASS  $*"; }
  fail() {
    csay "FAIL  $*"
    fails=$((fails + 1))
  }
  warn() {
    csay "WARN  $*"
    warns=$((warns + 1))
  }
  t_step=$(m0b_step_time)
  csay "# $sc: collected $(date -u +%FT%TZ) from $STATE; step at $(date -u -d "@${t_step%.*}" +%T)Z"
  m0b_versions "$out/versions-after.txt"
  wn=$(cid "$P" whalenurse)
  [[ -n "$wn" ]] || { fail "the whalenurse container is gone"; csay "RESULT: FAIL"; return 1; }
  k_parent=$(recreate_key parent)
  k_sidecar=$(recreate_key sidecar)

  csay "== waiting up to ${watch}s for the parent and the sidecar to be recreated and healthy"
  for ((i = 0; i < watch; i += 5)); do
    p_id=$(cid "$P" parent)
    s_id=$(cid "$P" sidecar)
    # shellcheck disable=SC2154
    if [[ -n "$p_id" && -n "$s_id" && "$p_id" != "$baseline_parent" && "$s_id" != "$baseline_sidecar" ]] && is_healthy "$p_id" && is_healthy "$s_id"; then break; fi
    sleep 5
  done
  m0b_table "$P" >"$out/table-after.tsv"
  m0b_netns "$P" >"$out/netns-after.tsv"
  m0b_inspect_dir "$P" "$out/inspect-after"
  nurse_status "$wn" "$out/status-after.json" || warn "whalenurse does not answer on its control socket"
  nurse_logs "$wn" 0 "$out/whalenurse-log-all.ndjson"
  docker events --since "${t_step%.*}" --until "$(date +%s)" --filter "label=com.docker.compose.project=$P" --filter type=container --format '{{json .}}' >"$out/events.ndjson" 2>&1 || true
  {
    echo "# docker events since the step, by service"
    m0b_events_by_service <(jq -c 'select(.Action != null) | . + {timeNano: (.timeNano // 0)}' "$out/events.ndjson") '^(parent|sidecar)$' || true
  } >"$out/events-by-service.txt"
  tee -a "$sum" <"$out/events-by-service.txt" >/dev/null
  column -t -s$'\t' "$out/table-after.tsv" | tee -a "$sum"

  # 1. the parent: restart first (no help), then recreate
  p_id=$(cid "$P" parent)
  s_id=$(cid "$P" sidecar)
  acts=$(recreate_actions "$out/whalenurse-log-all.ndjson" "$k_parent")
  if [[ "$p_id" != "$baseline_parent" ]]; then pass "the parent was recreated (new container ID)"; else fail "the parent still has its old container ID"; fi
  if [[ "$acts" == restart,recreate ]]; then pass "whalenurse's actions on the parent: $acts"; else fail "whalenurse's actions on the parent: '$acts' (want restart,recreate)"; fi
  if is_healthy "$p_id"; then pass "the new parent is healthy"; else fail "the new parent is not healthy"; fi

  # 2. the sidecar: recreated against the new parent, working
  if [[ "$s_id" != "$baseline_sidecar" ]]; then pass "the sidecar was recreated (new container ID)"; else fail "the sidecar still has its old container ID: it was left stale or restarted"; fi
  if [[ "$(docker inspect --format '{{.HostConfig.NetworkMode}}' "$s_id")" == "container:$p_id" ]]; then
    pass "the sidecar's network_mode points at the new parent"
  else
    fail "the sidecar's network_mode is $(docker inspect --format '{{.HostConfig.NetworkMode}}' "$s_id"), the parent is $p_id"
  fi
  ns=$(awk -F'\t' '$1 == "sidecar" {print $3}' "$out/netns-after.tsv")
  if [[ "$ns" == same ]]; then pass "the sidecar shares the parent's network namespace"; else fail "network namespaces: ${ns:-?}"; fi
  if recreate_reaches "$s_id"; then pass "the sidecar reaches the parent's httpd on its own 127.0.0.1: it works"; else fail "the sidecar cannot reach the parent"; fi
  acts=$(recreate_actions "$out/whalenurse-log-all.ndjson" "$k_sidecar")
  if [[ "$acts" == recreate-stale ]]; then
    pass "whalenurse's only action on the sidecar: $acts (never an API restart or start)"
  else
    fail "whalenurse's actions on the sidecar: '$acts' (want recreate-stale)"
  fi

  # 3. the guard did not get in the way, and nothing was given up on
  if [[ "$(nurse_count "$out/whalenurse-log-all.ndjson" DriftBlocked "$k_parent")" == 0 && "$(nurse_count "$out/whalenurse-log-all.ndjson" DriftBlocked "$k_sidecar")" == 0 ]]; then
    pass "the drift guard blocked nothing"
  else
    fail "DriftBlocked: $(jq -r 'select(.type == "DriftBlocked") | .msg' "$out/whalenurse-log-all.ndjson" | head -3)"
  fi
  if [[ "$(nurse_count "$out/whalenurse-log-all.ndjson" Suspended "$k_parent")" == 0 && "$(nurse_count "$out/whalenurse-log-all.ndjson" Suspended "$k_sidecar")" == 0 ]]; then
    pass "nothing was SUSPENDED"
  else
    fail "something was SUSPENDED"
  fi
  if ! jq -e 'select(.type == "DependentPostCheck")' "$out/whalenurse-log-all.ndjson" >/dev/null 2>&1; then
    csay "info  the non-forced up recreated the sidecar by itself (no --force-recreate post-check)"
  else
    warn "the post-check had to recreate the sidecar with --force-recreate: $(jq -r 'select(.type == "DependentPostCheck") | .msg' "$out/whalenurse-log-all.ndjson" | head -1)"
  fi
  if [[ "$(jq -r '.compose.source' "$out/status-after.json" 2>/dev/null)" == host ]]; then pass "whalenurse used the host compose plugin"; else warn "compose source: $(jq -r '.compose.source' "$out/status-after.json" 2>/dev/null)"; fi
  # shellcheck disable=SC2154
  if [[ "$(docker inspect --format '{{.State.StartedAt}}' "$wn")" == "$nurse_started" ]]; then pass "whalenurse itself was not restarted"; else warn "whalenurse was restarted during the scenario"; fi

  # 4. nothing else on the host was touched
  docker ps -aq --no-trunc --format '{{.ID}} {{.Label "com.docker.compose.project"}}' | awk -v p="$P" '$2 != p {print $1}' | sort >"$out/others.txt"
  if diff -q "$STATE/baseline/others.txt" "$out/others.txt" >/dev/null; then pass "the containers outside $P are exactly the ones from before"; else fail "containers outside $P changed: $(diff "$STATE/baseline/others.txt" "$out/others.txt" | head -5)"; fi

  if ((fails == 0)); then csay "RESULT: PASS ($warns WARN)"; else csay "RESULT: FAIL ($fails FAIL, $warns WARN)"; fi
  csay "evidence: $out (raw: stays in captures/, never committed)"
  ((fails == 0))
}
