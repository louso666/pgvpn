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

require_cmd() {
  for cmd in "$@"; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      echo "Missing dependency: $cmd" >&2
      exit 1
    fi
  done
}

require_cmd ssh scp mktemp awk date

fetch_ipset() {
  local set_name="$1"
  ssh $PG_SSH_OPTS -p "$PG_SSH_PORT" "${PG_SSH_USER}@${PG_SSH_HOST}" \
    "ipset save $set_name" | awk '/^add/{print $3}'
}

generate_rsc() {
  local router_id="$1" tmp_file="$2"
  local list_nl="${MIKROTIK_LIST_NL:-pg-proxy-nl}"
  local list_usa="${MIKROTIK_LIST_USA:-pg-proxy-usa}"
  local comment_nl="${MIKROTIK_COMMENT_NL:-pg-sync nl}"
  local comment_usa="${MIKROTIK_COMMENT_USA:-pg-sync usa}"
  local route_comment="${MIKROTIK_ROUTE_COMMENT:-pg-sync to-pg}"
  local routing_mark_var="ROUTER_${router_id}_ROUTING_MARK"
  local gateway_var="ROUTER_${router_id}_GATEWAY"

  local routing_mark="${!routing_mark_var}"
  local gateway="${!gateway_var}"

  if [[ -z "$routing_mark" || -z "$gateway" ]]; then
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
    done < <(fetch_ipset nl_proxy)

    while IFS= read -r ip; do
      [[ -z "$ip" ]] && continue
      printf "/ip firewall address-list add list=\"%s\" address=%s disabled=no comment=\"%s\"\n" "$list_usa" "$ip" "$comment_usa"
    done < <(fetch_ipset usa_proxy)

    printf "/ip firewall mangle remove [find comment=\"%s\"]\n" "$comment_nl"
    printf "/ip firewall mangle remove [find comment=\"%s\"]\n" "$comment_usa"
    printf "/ip firewall mangle add chain=prerouting dst-address-list=\"%s\" action=mark-routing new-routing-mark=\"%s\" passthrough=no comment=\"%s\"\n" "$list_nl" "$routing_mark" "$comment_nl"
    printf "/ip firewall mangle add chain=prerouting dst-address-list=\"%s\" action=mark-routing new-routing-mark=\"%s\" passthrough=no comment=\"%s\"\n" "$list_usa" "$routing_mark" "$comment_usa"

    printf "/ip route remove [find comment=\"%s\"]\n" "$route_comment"
    printf "/ip route add dst-address=0.0.0.0/0 gateway=%s routing-mark=\"%s\" distance=1 check-gateway=ping comment=\"%s\"\n" "$gateway" "$routing_mark" "$route_comment"
  } >"$tmp_file"
}

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
    exit 1
  fi

  tmp_rsc=$(mktemp)
  generate_rsc "$router_id" "$tmp_rsc"

  remote_name="pg-sync.rsc"
  echo "[${router_id}] uploading address list (${tmp_rsc})"
  scp $sshopts -P "$port" "$tmp_rsc" "${user}@${host}:$remote_name"
  echo "[${router_id}] importing $remote_name"
  ssh $sshopts -p "$port" "${user}@${host}" "/import file-name=$remote_name" >/dev/null
  echo "[${router_id}] done"
  rm -f "$tmp_rsc"
done
