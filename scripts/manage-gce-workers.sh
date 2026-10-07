#!/usr/bin/env bash
# Manage GCE VMs used as truffles scan workers.
#
# Usage:
#   ./scripts/manage-gce-workers.sh <PROJECT_ID> <command> [options]
#   ./scripts/manage-gce-workers.sh sandbox420 suggest
#   ./scripts/manage-gce-workers.sh sandbox420 list
#   ./scripts/manage-gce-workers.sh sandbox420 create --count 2 --machine e2-standard-2
#   ./scripts/manage-gce-workers.sh sandbox420 env truffles-worker-1 1
#
# Workers are tagged with labels: app=truffles, role=scan-worker
# so `list` only shows truffles nodes (not the whole project).
#
# Auth: uses the active user from `gcloud auth login` (not Application Default
# Credentials). GOOGLE_APPLICATION_CREDENTIALS is ignored for these calls.
set -euo pipefail

LABEL_APP="truffles"
LABEL_ROLE="scan-worker"
DEFAULT_ZONE="us-central1-a"
DEFAULT_MACHINE="e2-standard-2"
DEFAULT_DISK_GB=50
DEFAULT_IMAGE_FAMILY="ubuntu-2204-lts"
DEFAULT_IMAGE_PROJECT="ubuntu-os-cloud"
NAME_PREFIX="truffles-worker"
ACCOUNT="" # set by ensure_gcloud_user_auth, or --account=

die() { echo "error: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "'$1' not on PATH"; }

usage() {
  cat <<'EOF'
manage-gce-workers.sh — create / list / resize GCE scan workers for truffles

usage:
  ./scripts/manage-gce-workers.sh <PROJECT_ID> <command> [options]
  ./scripts/manage-gce-workers.sh <PROJECT_ID> --account you@gmail.com list

auth:
  Uses `gcloud auth login` user credentials (active account).
  Does NOT use Application Default Credentials / GOOGLE_APPLICATION_CREDENTIALS.
  If you see "Reauthentication required", run:
    gcloud auth login
    gcloud auth list   # confirm ACTIVE account

commands:
  suggest              Machine-size guidance for scan concurrency
  list                 List workers labeled app=truffles,role=scan-worker
  create               Create one or more workers
      --count N          how many (default 1)
      --machine TYPE     e2-small|e2-medium|e2-standard-2|e2-standard-4|… (default e2-standard-2)
      --zone ZONE        default us-central1-a
      --disk GB          boot disk GiB (default 50; clones need space)
      --name NAME        single instance name (default truffles-worker[-N])
  start NAME...        Start stopped worker(s)
  stop NAME...         Stop worker(s) (keeps disk; saves money)
  reset NAME...        Hard reset (OOM / wedged SSH)
  resize NAME --machine TYPE
                       Stop, change machine type, start
  delete NAME...       Delete instance(s) (boot disk deleted too)
  ssh NAME             gcloud compute ssh into a worker
  env NAME [INDEX]     Print TRUFFLES_WORKER* lines for remote.env / remote-multi .env

examples:
  ./scripts/manage-gce-workers.sh sandbox420 suggest
  ./scripts/manage-gce-workers.sh sandbox420 create --count 2 --machine e2-standard-2
  ./scripts/manage-gce-workers.sh sandbox420 list
  ./scripts/manage-gce-workers.sh sandbox420 env truffles-worker-1 1 >> remote-multi.env
  ./truffles cluster deploy -c examples/playbook.remote-multi.yaml
EOF
}

suggest() {
  cat <<'EOF'
Scan throughput is clone-bound (full-history git mirrors). Size for RAM + disk,
then scale out with more VMs — cluster run scans all slaves in parallel.

  machine          vCPU   RAM    suggested scan.workers   notes
  ───────────────  ─────  ─────  ───────────────────────  ─────────────────────────────
  e2-small         2      2 GB   2                        cheapest; OOMs above ~2
  e2-medium        2      4 GB   2–3                      ok for light lists
  e2-standard-2    2      8 GB   3–4                      recommended default ★
  e2-standard-4    4     16 GB   6–8                      fewer, fatter hosts
  e2-standard-8    8     32 GB   10–12                    usually worse $/throughput
                                                          than 2–3× e2-standard-2

Horizontal (preferred for 100+ repos):
  2–3 × e2-standard-2  →  playbook with multiple slaves (see examples/playbook.remote-multi.yaml)
  Keep scan.no_proxy: true (direct clones). Proxies help search, not big packs.

Disk: ≥50 GB SSD boot disk. Concurrent mirrors of large LLM repos eat /tmp quickly.

After create:
  ./scripts/manage-gce-workers.sh PROJECT env NAME [N]   # paste into *.env
  ./truffles cluster deploy -c your-playbook.yaml
  ./truffles playbook -f your-playbook.yaml
EOF
}

# Pin user login creds; strip ADC so a stale JSON key cannot take over.
ensure_gcloud_user_auth() {
  # ADC is for client libraries — gcloud compute should use auth login.
  unset GOOGLE_APPLICATION_CREDENTIALS
  unset CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE
  unset CLOUDSDK_AUTH_ACCESS_TOKEN

  if [[ -z "$ACCOUNT" ]]; then
    ACCOUNT="$(gcloud auth list --filter=status:ACTIVE --format='value(account)' 2>/dev/null | head -n1 || true)"
  fi
  if [[ -z "$ACCOUNT" ]]; then
    ACCOUNT="$(gcloud config get-value account 2>/dev/null || true)"
  fi
  if [[ -z "$ACCOUNT" || "$ACCOUNT" == "(unset)" ]]; then
    die "no active gcloud account. Run: gcloud auth login"
  fi

  # Cheap probe: if this fails with reauth, tell the user to refresh login
  # instead of hanging on repeated "Please enter your password" prompts.
  if ! CLOUDSDK_CORE_DISABLE_PROMPTS=1 gcloud --account="$ACCOUNT" auth print-access-token >/dev/null 2>&1; then
    die "gcloud user credentials expired or need reauth for ${ACCOUNT}.
A stale CLOUDSDK_AUTH_ACCESS_TOKEN in the shell causes password prompts
instead of a browser. In this terminal run:
  unset CLOUDSDK_AUTH_ACCESS_TOKEN GOOGLE_APPLICATION_CREDENTIALS
  gcloud auth login ${ACCOUNT} --launch-browser
  # if no window opens:
  # gcloud auth login ${ACCOUNT} --no-launch-browser
  gcloud auth list
Then re-run this script."
  fi
  echo "[*] gcloud account: ${ACCOUNT} (from auth login)" >&2
}

gcloud_p() {
  # Never inherit ADC for worker management.
  env -u GOOGLE_APPLICATION_CREDENTIALS \
      -u CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE \
      -u CLOUDSDK_AUTH_ACCESS_TOKEN \
    gcloud --account="$ACCOUNT" --project="$PROJECT" "$@"
}

list_workers() {
  local zone_filter="${1:-}"
  local fmt='table(name,zone.basename(),status,machineType.basename(),networkInterfaces[0].accessConfigs[0].natIP:label=EXTERNAL_IP)'
  if [[ -n "$zone_filter" ]]; then
    gcloud_p compute instances list \
      --zones="$zone_filter" \
      --filter="labels.app=${LABEL_APP} AND labels.role=${LABEL_ROLE}" \
      --format="$fmt"
  else
    gcloud_p compute instances list \
      --filter="labels.app=${LABEL_APP} AND labels.role=${LABEL_ROLE}" \
      --format="$fmt"
  fi
}

instance_zone() {
  local name="$1"
  gcloud_p compute instances list \
    --filter="name=${name} AND labels.app=${LABEL_APP}" \
    --format='value(zone.basename())' | head -n1
}

require_zone_for() {
  local name="$1"
  local z
  z="$(instance_zone "$name")"
  [[ -n "$z" ]] || die "instance not found (or missing truffles labels): $name"
  echo "$z"
}

create_workers() {
  local count=1 machine="$DEFAULT_MACHINE" zone="$DEFAULT_ZONE" disk="$DEFAULT_DISK_GB" name=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --count) count="${2:?}"; shift 2 ;;
      --machine) machine="${2:?}"; shift 2 ;;
      --zone) zone="${2:?}"; shift 2 ;;
      --disk) disk="${2:?}"; shift 2 ;;
      --name) name="${2:?}"; shift 2 ;;
      *) die "unknown create option: $1" ;;
    esac
  done
  [[ "$count" =~ ^[1-9][0-9]*$ ]] || die "--count must be a positive integer"

  case "$machine" in
    e2-small)
      echo "note: e2-small (~2GB) — keep playbook scan.workers ≤ 2 or expect OOM/SIGKILL" >&2
      ;;
    e2-medium)
      echo "note: e2-medium (~4GB) — suggested scan.workers 2–3" >&2
      ;;
    e2-standard-2)
      echo "note: e2-standard-2 (~8GB) — suggested scan.workers 3–4 (recommended)" >&2
      ;;
    e2-standard-4|e2-standard-8)
      echo "note: $machine — suggested scan.workers $([ "$machine" = e2-standard-4 ] && echo 6-8 || echo 10-12)" >&2
      ;;
    *)
      echo "note: unknown machine $machine — check RAM before raising scan.workers" >&2
      ;;
  esac

  local i created=()
  for ((i = 1; i <= count; i++)); do
    local inst="$name"
    if [[ -z "$inst" ]]; then
      if [[ "$count" -eq 1 ]]; then
        inst="$NAME_PREFIX"
      else
        inst="${NAME_PREFIX}-${i}"
      fi
    elif [[ "$count" -gt 1 ]]; then
      inst="${name}-${i}"
    fi

    echo "[*] creating ${inst} (${machine}, ${zone}, ${disk}GB)…"
    gcloud_p compute instances create "$inst" \
      --zone="$zone" \
      --machine-type="$machine" \
      --boot-disk-size="${disk}GB" \
      --boot-disk-type=pd-balanced \
      --image-family="$DEFAULT_IMAGE_FAMILY" \
      --image-project="$DEFAULT_IMAGE_PROJECT" \
      --labels="app=${LABEL_APP},role=${LABEL_ROLE}" \
      --tags=truffles-worker \
      --quiet
    created+=("$inst")
  done

  echo
  echo "[+] created ${#created[@]} worker(s). Env snippets:"
  local snippet
  snippet="$(mktemp "${TMPDIR:-/tmp}/truffles-gce-env.XXXXXX")"
  # shellcheck disable=SC2064
  trap "rm -f '$snippet'" RETURN

  local idx=1
  local use_index=1
  # Single worker → TRUFFLES_WORKER_* .
  # Multiple → TRUFFLES_WORKER1_*, TRUFFLES_WORKER2_*, …
  if [[ ${#created[@]} -eq 1 ]]; then
    use_index=0
  fi
  for inst in "${created[@]}"; do
    if [[ "$use_index" -eq 0 ]]; then
      print_env "$inst" "" | tee -a "$snippet"
    else
      print_env "$inst" "$idx" | tee -a "$snippet"
    fi
    echo | tee -a "$snippet" >/dev/null
    echo
    idx=$((idx + 1))
  done

  offer_write_env "$snippet" "${#created[@]}"

  echo "Next:"
  echo "  point playbook env_file at that .env and add one slaves: entry per WORKER*N*"
  echo "  ./truffles cluster deploy -c <playbook.yaml>   # ships truffles + installs trufflehog"
}

# Interactive: ask which env file to append worker vars into (create if missing).
offer_write_env() {
  local snippet="$1"
  local nworkers="${2:-1}"

  if [[ ! -t 0 || ! -t 1 ]]; then
    echo "note: non-interactive shell — skipped env-file prompt; paste snippets manually." >&2
    return 0
  fi

  local ans
  read -r -p "Add these vars to an env file? [Y/n] " ans || true
  case "${ans:-Y}" in
    n|N|no|NO) return 0 ;;
  esac

  local default_path="remote.env"
  if [[ -f remote.env ]]; then
    default_path="remote.env"
  elif [[ -f remote-single.env ]]; then
    default_path="remote-single.env"
  elif [[ -f examples/remote-multi.env ]]; then
    default_path="examples/remote-multi.env"
  fi

  local path
  read -r -p "Env file path [${default_path}]: " path || true
  path="${path:-$default_path}"
  # Expand leading ~/
  if [[ "$path" == ~/* ]]; then
    path="${HOME}/${path#~/}"
  fi

  if [[ ! -f "$path" ]]; then
    read -r -p "File does not exist. Create ${path}? [Y/n] " ans || true
    case "${ans:-Y}" in
      n|N|no|NO)
        echo "skipped (no file written)." >&2
        return 0
        ;;
    esac
    local dir
    dir="$(dirname "$path")"
    if [[ "$dir" != "." && ! -d "$dir" ]]; then
      mkdir -p "$dir"
    fi
    {
      echo "# Local secrets / machine-specific (gitignored)."
      echo "# Written by manage-gce-workers.sh $(date -u +%Y-%m-%dT%H:%MZ)"
      echo
      cat "$snippet"
    } >"$path"
    echo "[+] created ${path}"
  else
    read -r -p "Append to existing ${path}? [Y/n] " ans || true
    case "${ans:-Y}" in
      n|N|no|NO)
        echo "skipped (no file written)." >&2
        return 0
        ;;
    esac
    {
      echo
      echo "# --- manage-gce-workers $(date -u +%Y-%m-%dT%H:%MZ) ---"
      cat "$snippet"
    } >>"$path"
    echo "[+] appended to ${path}"
  fi
}

print_env() {
  local name="$1"
  local index="${2:-}"
  local zone ip user key workdir
  zone="$(require_zone_for "$name")"
  ip="$(gcloud_p compute instances describe "$name" --zone="$zone" \
    --format='get(networkInterfaces[0].accessConfigs[0].natIP)')"
  [[ -n "$ip" ]] || die "no external IP on $name — check firewall / access config"
  user="${USER:-$(whoami)}"
  key="${HOME}/.ssh/google_compute_engine"
  workdir="/home/${user}/truffles-work"

  if [[ -z "$index" || "$index" == "0" ]]; then
    cat <<EOF
TRUFFLES_WORKER_HOST=${ip}
TRUFFLES_WORKER_USER=${user}
TRUFFLES_WORKER_KEY=\${HOME}/.ssh/google_compute_engine
TRUFFLES_WORKER_WORKDIR=${workdir}
TRUFFLES_WORKER_BIN=/usr/local/bin/truffles
TRUFFLES_WORKER_GCE_INSTANCE=${name}
TRUFFLES_WORKER_GCE_ZONE=${zone}
TRUFFLES_WORKER_GCE_PROJECT=${PROJECT}
EOF
  else
    cat <<EOF
TRUFFLES_WORKER${index}_HOST=${ip}
TRUFFLES_WORKER${index}_USER=${user}
TRUFFLES_WORKER${index}_KEY=\${HOME}/.ssh/google_compute_engine
TRUFFLES_WORKER${index}_WORKDIR=${workdir}
TRUFFLES_WORKER${index}_BIN=/usr/local/bin/truffles
TRUFFLES_WORKER${index}_GCE_INSTANCE=${name}
TRUFFLES_WORKER${index}_GCE_ZONE=${zone}
TRUFFLES_WORKER${index}_GCE_PROJECT=${PROJECT}
EOF
  fi
}

op_names() {
  local op="$1"
  shift
  [[ $# -gt 0 ]] || die "$op requires instance NAME(s)"
  local name zone
  for name in "$@"; do
    zone="$(require_zone_for "$name")"
    echo "[*] ${op} ${name} (${zone})…"
    case "$op" in
      start) gcloud_p compute instances start "$name" --zone="$zone" --quiet ;;
      stop) gcloud_p compute instances stop "$name" --zone="$zone" --quiet ;;
      reset) gcloud_p compute instances reset "$name" --zone="$zone" --quiet ;;
      delete)
        gcloud_p compute instances delete "$name" --zone="$zone" --quiet
        ;;
    esac
  done
}

resize_one() {
  local name="" machine=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --machine) machine="${2:?}"; shift 2 ;;
      -*) die "unknown resize option: $1" ;;
      *)
        [[ -z "$name" ]] || die "resize takes one NAME"
        name="$1"
        shift
        ;;
    esac
  done
  [[ -n "$name" ]] || die "resize requires NAME"
  [[ -n "$machine" ]] || die "resize requires --machine TYPE"
  local zone
  zone="$(require_zone_for "$name")"
  echo "[*] stopping ${name}…"
  gcloud_p compute instances stop "$name" --zone="$zone" --quiet
  echo "[*] set machine-type ${machine}…"
  gcloud_p compute instances set-machine-type "$name" --zone="$zone" --machine-type="$machine" --quiet
  echo "[*] starting ${name}…"
  gcloud_p compute instances start "$name" --zone="$zone" --quiet
  echo "[+] ${name} is now ${machine}"
  print_env "$name"
}

ssh_worker() {
  local name="${1:?ssh requires NAME}"
  local zone
  zone="$(require_zone_for "$name")"
  exec gcloud_p compute ssh "$name" --zone="$zone"
}

# --- main ---
need gcloud

if [[ $# -lt 1 ]]; then
  usage
  exit 1
fi

case "${1:-}" in
  -h|--help|help) usage; exit 0 ;;
esac

PROJECT="${1:?project id required}"
shift

# Optional global: --account you@gmail.com before the command
while [[ $# -gt 0 ]]; do
  case "$1" in
    --account)
      ACCOUNT="${2:?}"
      shift 2
      ;;
    -h|--help|help)
      usage
      exit 0
      ;;
    *)
      break
      ;;
  esac
done

[[ $# -ge 1 ]] || { usage; exit 1; }
CMD="$1"
shift

case "$CMD" in
  -h|--help|help) usage; exit 0 ;;
  suggest) suggest; exit 0 ;;
esac

ensure_gcloud_user_auth

case "$CMD" in
  list) list_workers "$@" ;;
  create) create_workers "$@" ;;
  start|stop|reset|delete) op_names "$CMD" "$@" ;;
  resize) resize_one "$@" ;;
  ssh) ssh_worker "$@" ;;
  env)
    [[ $# -ge 1 ]] || die "env requires NAME [INDEX]"
    print_env "$1" "${2:-}"
    ;;
  *)
    usage
    die "unknown command: $CMD"
    ;;
esac
