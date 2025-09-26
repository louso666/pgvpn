#!/usr/bin/env bash
set -euo pipefail

CONFIG_FILE="${CONFIG_FILE:-$(dirname "${BASH_SOURCE[0]}")/mikrotik-sync.conf}"
if [[ ! -f "$CONFIG_FILE" ]]; then
  echo "Config file not found: $CONFIG_FILE" >&2
  exit 1
fi
# shellcheck disable=SC1090
source "$CONFIG_FILE"

PG_SSH_PORT=${PG_SSH_PORT:-22}
PG_SSH_OPTS=${PG_SSH_OPTS:-}
SSH_BASE_OPTS="-o BatchMode=yes -o ConnectTimeout=10"

require_cmd() {
  for cmd in "$@"; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      echo "Missing dependency: $cmd" >&2
      exit 1
    fi
  done
}

require_cmd ssh scp mktemp awk date ipset hostname

is_local_host() {
  local host="$1"
  [[ -z "$host" ]] && return 0
  case "$host" in
    localhost|127.0.0.1) return 0 ;;
  esac
  local short="$(hostname)"
  local fqdn="$(hostname -f 2>/dev/null || echo "$short")"
  [[ "$host" == "$short" || "$host" == "$fqdn" ]] && return 0
  return 1
}

fetch_ipset() {
  local set_name="$1"
  if is_local_host "${PG_SSH_HOST:-}"; then
    ipset save "$set_name" | awk '/^add/{print $3}'
  else
    ssh $SSH_BASE_OPTS $PG_SSH_OPTS -p "$PG_SSH_PORT" "${PG_SSH_USER}@${PG_SSH_HOST}" \
      "ipset save $set_name" | awk '/^add/{print $3}'
  fi
}

generate_rsc() {
  local router_id="$1" tmp_file="$2" nl_file="$3" usa_file="$4"
  local list_nl="${MIKROTIK_LIST_NL:-pg-proxy-nl}"
  local list_usa="${MIKROTIK_LIST_USA:-pg-proxy-usa}"
  local comment_nl="${MIKROTIK_COMMENT_NL:-pg-sync nl}"
  local comment_usa="${MIKROTIK_COMMENT_USA:-pg-sync usa}"
  local route_comment="${MIKROTIK_ROUTE_COMMENT:-pg-sync to-pg}"
  local routing_mark_var="ROUTER_${router_id}_ROUTING_MARK"
  local gateway_var="ROUTER_${router_id}_GATEWAY"

  local routing_mark="${!routing_mark_var:-}"
  local gateway="${!gateway_var:-}"

  if [[ -z "${routing_mark:-}" || -z "${gateway:-}" ]]; then
    echo "Routing mark or gateway not defined for router $router_id" >&2
    return 1
  fi

  {
    printf "# generated %s by sync-mikrotik-address-lists\n" "$(date -Is)"
    printf "/ip firewall address-list remove [find list=\"%s\"]\n" "$list_nl"
    printf "/ip firewall address-list remove [find list=\"%s\"]\n" "$list_usa"

    while IFS= read -r ip; do
      [[ -z "$ip" ]] && continue
      printf "/ip firewall address-list add list=\"%s\" address=%s disabled=no comment=\"%s\"\n" "$list_nl" "$ip" "$comment_nl"
    done <"$nl_file"

    while IFS= read -r ip; do
      [[ -z "$ip" ]] && continue
      printf "/ip firewall address-list add list=\"%s\" address=%s disabled=no comment=\"%s\"\n" "$list_usa" "$ip" "$comment_usa"
    done <"$usa_file"

    printf "/ip firewall mangle remove [find comment=\"%s\"]\n" "$comment_nl"
    printf "/ip firewall mangle remove [find comment=\"%s\"]\n" "$comment_usa"
    printf "/ip firewall mangle add chain=prerouting dst-address-list=\"%s\" action=mark-routing new-routing-mark=\"%s\" passthrough=no comment=\"%s\"\n" "$list_nl" "$routing_mark" "$comment_nl"
    printf "/ip firewall mangle add chain=prerouting dst-address-list=\"%s\" action=mark-routing new-routing-mark=\"%s\" passthrough=no comment=\"%s\"\n" "$list_usa" "$routing_mark" "$comment_usa"

    printf "/ip route remove [find comment=\"%s\"]\n" "$route_comment"
    printf "/ip route add dst-address=0.0.0.0/0 gateway=%s routing-mark=\"%s\" distance=1 check-gateway=ping comment=\"%s\"\n" "$gateway" "$routing_mark" "$route_comment"
  } >"$tmp_file"
}

NL_TMP=$(mktemp)
USA_TMP=$(mktemp)
trap 'rm -f "$NL_TMP" "$USA_TMP"' EXIT

fetch_ipset nl_proxy >"$NL_TMP"
fetch_ipset usa_proxy >"$USA_TMP"

for router_id in "${MIKROTIK_ROUTERS[@]}"; do
  host_var="ROUTER_${router_id}_HOST"
  user_var="ROUTER_${router_id}_USER"
  port_var="ROUTER_${router_id}_PORT"
  sshopts_var="ROUTER_${router_id}_SSH_OPTS"

  host="${!host_var:-}"
  user="${!user_var:-}"
  port="${!port_var:-22}"
  sshopts="${!sshopts_var:-}"

  if [[ -z "$host" || -z "$user" ]]; then
    echo "Router $router_id is missing host or user in config" >&2
    continue
  fi

  tmp_rsc=$(mktemp)
  if ! generate_rsc "$router_id" "$tmp_rsc" "$NL_TMP" "$USA_TMP"; then
    rm -f "$tmp_rsc"
    continue
  fi

  remote_name="pg-sync.rsc"
  echo "[${router_id}] uploading address list (${tmp_rsc})"
  if ! scp $SSH_BASE_OPTS $sshopts -P "$port" "$tmp_rsc" "${user}@${host}:$remote_name"; then
    echo "[${router_id}] scp failed" >&2
    rm -f "$tmp_rsc"
    continue
  fi
  echo "[${router_id}] importing $remote_name"
  if ! ssh $SSH_BASE_OPTS $sshopts -p "$port" "${user}@${host}" "/import file-name=$remote_name" >/dev/null; then
    echo "[${router_id}] import failed" >&2
  else
    echo "[${router_id}] done"
  fi
  rm -f "$tmp_rsc"
done
