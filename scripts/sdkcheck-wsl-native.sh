#!/usr/bin/env bash
set -euo pipefail

RUN_TESTS=1
RUN_CLI=1
RESET=1
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_AS_USER="${SUDO_USER:-${SDKCHECK_RUN_AS_USER:-}}"

usage() {
  cat <<'USAGE'
Usage: scripts/sdkcheck-wsl-native.sh [options]

Resets native WSL OVN/OVS databases, exposes OVSDB TCP endpoints, and runs the
external ovnflow SDK checker module.

WARNING: by default this removes the native WSL OVN/OVS database files:
  /var/lib/openvswitch/conf.db
  /var/lib/ovn/ovnnb_db.db
  /var/lib/ovn/ovnsb_db.db

Options:
  --no-reset     Do not reset existing native WSL OVN/OVS databases.
  --no-test      Skip go test ./...
  --no-cli       Skip go run ./cmd/sdkcheck
  -h, --help     Show this help.
USAGE
}

while (($#)); do
  case "$1" in
    --no-reset) RESET=0 ;;
    --no-test) RUN_TESTS=0 ;;
    --no-cli) RUN_CLI=0 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

log() {
  printf '\n==> %s\n' "$*"
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    return 1
  fi
}

missing=0
for cmd in go ovs-vsctl ovn-nbctl ovn-sbctl ovsdb-client ovsdb-tool systemctl timeout; do
  need_cmd "$cmd" || missing=1
done
if [[ "${EUID:-$(id -u)}" != "0" ]]; then
  need_cmd sudo || missing=1
fi
if ((missing != 0)); then
  cat >&2 <<'HINT'

Install the native WSL dependencies, for example:
  sudo apt-get update
  sudo apt-get install -y openvswitch-switch ovn-central ovn-common golang-go
HINT
  exit 1
fi

run_root() {
  if [[ "${EUID:-$(id -u)}" == "0" ]]; then
    "$@"
  else
    sudo -n "$@"
  fi
}

if ((RESET == 1)); then
  log "Resetting native WSL OVN/OVS databases"
  if [[ "${EUID:-$(id -u)}" != "0" ]] && ! sudo -n true 2>/dev/null; then
    echo "This script needs passwordless sudo inside WSL, or run it from a root WSL shell." >&2
    echo "Refusing to block on an interactive sudo prompt." >&2
    exit 1
  fi
  run_root timeout 20s systemctl stop ovn-central 2>/dev/null || true
  run_root timeout 20s systemctl stop openvswitch-switch 2>/dev/null || true
  run_root pkill -f 'ovn-northd|ovn-controller|ovnnb_db|ovnsb_db|ovs-vswitchd|ovsdb-server' 2>/dev/null || true
  run_root rm -f /var/lib/openvswitch/conf.db /etc/openvswitch/conf.db
  run_root rm -f /var/lib/ovn/ovnnb_db.db /var/lib/ovn/ovnsb_db.db
  run_root mkdir -p /var/lib/openvswitch /etc/openvswitch /var/lib/ovn
  run_root ovsdb-tool create /var/lib/openvswitch/conf.db /usr/share/openvswitch/vswitch.ovsschema
  run_root ln -sf /var/lib/openvswitch/conf.db /etc/openvswitch/conf.db
  run_root ovsdb-tool create /var/lib/ovn/ovnnb_db.db /usr/share/ovn/ovn-nb.ovsschema
  run_root ovsdb-tool create /var/lib/ovn/ovnsb_db.db /usr/share/ovn/ovn-sb.ovsschema
fi

log "Starting native WSL OVN/OVS services"
run_root timeout 30s systemctl start openvswitch-switch
run_root timeout 30s systemctl start ovn-central

wait_unix_db() {
  local endpoint="$1"
  local database="$2"
  for i in $(seq 1 60); do
    if run_root ovsdb-client list-dbs "$endpoint" 2>/dev/null | grep -qx "$database"; then
      echo "$database is ready at $endpoint"
      return 0
    fi
    sleep 1
  done
  echo "$database at $endpoint did not become ready" >&2
  return 1
}

log "Waiting for native Unix sockets"
wait_unix_db unix:/var/run/openvswitch/db.sock Open_vSwitch
wait_unix_db unix:/var/run/ovn/ovnnb_db.sock OVN_Northbound
wait_unix_db unix:/var/run/ovn/ovnsb_db.sock OVN_Southbound

log "Exposing OVSDB TCP endpoints"
run_root ovs-vsctl --no-wait init
run_root ovs-vsctl set-manager ptcp:6640:0.0.0.0
run_root ovn-nbctl set-connection ptcp:6641:0.0.0.0
run_root ovn-sbctl set-connection ptcp:6642:0.0.0.0

wait_db() {
  local endpoint="$1"
  local database="$2"
  for i in $(seq 1 120); do
    if ovsdb-client list-dbs "$endpoint" 2>/dev/null | grep -qx "$database"; then
      echo "$database is ready at $endpoint"
      return 0
    fi
    if [[ "$i" == "1" ]] || ((i % 10 == 0)); then
      echo "Waiting for $database at $endpoint ($i/120)"
    fi
    sleep 1
  done
  echo "$database at $endpoint did not become ready" >&2
  ss -lntp | grep -E '6640|6641|6642' || true
  return 1
}

log "Waiting for OVSDB databases"
wait_db tcp:127.0.0.1:6640 Open_vSwitch
wait_db tcp:127.0.0.1:6641 OVN_Northbound
wait_db tcp:127.0.0.1:6642 OVN_Southbound

export OVNFLOW_OVS_ADDR=tcp:127.0.0.1:6640
export OVNFLOW_OVN_NB_ADDR=tcp:127.0.0.1:6641
export OVNFLOW_OVN_SB_ADDR=tcp:127.0.0.1:6642
export OVNFLOW_SDKCHECK_PREFIX="${OVNFLOW_SDKCHECK_PREFIX:-ovnflow-sdkcheck-}"
export OVNFLOW_SDKCHECK_BRIDGE="${OVNFLOW_SDKCHECK_BRIDGE:-br-ovnflow-sdkcheck}"

cd "$REPO_DIR/tools/sdkcheck"
run_go() {
  if [[ "${EUID:-$(id -u)}" == "0" && -n "$RUN_AS_USER" && "$RUN_AS_USER" != "root" ]]; then
    runuser -u "$RUN_AS_USER" -- "$@"
  else
    "$@"
  fi
}

if ((RUN_TESTS == 1)); then
  log "Running external SDK go tests"
  run_go go test -count=1 ./...
fi
if ((RUN_CLI == 1)); then
  log "Running external SDK CLI checker"
  run_go go run ./cmd/sdkcheck
fi

wsl_ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
log "SDK checker completed"
echo "Windows endpoints:"
echo "  OVNFLOW_OVS_ADDR=tcp:${wsl_ip:-127.0.0.1}:6640"
echo "  OVNFLOW_OVN_NB_ADDR=tcp:${wsl_ip:-127.0.0.1}:6641"
echo "  OVNFLOW_OVN_SB_ADDR=tcp:${wsl_ip:-127.0.0.1}:6642"
