#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# setup-bench.sh — Start source + dest PG containers and seed 50GB into source
#
# Usage:
#   ./scripts/setup-bench.sh            # full 50GB seed
#   ./scripts/setup-bench.sh --size 5   # override to 5GB (for quick tests)
#   ./scripts/setup-bench.sh --down     # tear down containers
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.bench.yml"

TARGET_GB=${SIZE:-25}
BATCH_SIZE=500000
APPROX_ROW_BYTES=512

detect_cpus() {
  local cpus
  cpus=$(sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 4)
  echo "$cpus"
}

TOTAL_CPUS=$(detect_cpus)
NUM_TABLES=5
WORKERS_PER_TABLE=$((TOTAL_CPUS / NUM_TABLES))
if ((WORKERS_PER_TABLE < 1)); then WORKERS_PER_TABLE=1; fi
if ((WORKERS_PER_TABLE > 4)); then WORKERS_PER_TABLE=4; fi

SOURCE_DSN="postgres://postgres:source@localhost:55432/source"

# ── Helpers ──────────────────────────────────────────────────────────────────

log() { printf "\033[1;34m==> %s\033[0m\n" "$*"; }
ok() { printf "\033[1;32m  ✓ %s\033[0m\n" "$*"; }
err() { printf "\033[1;31m  ✗ %s\033[0m\n" "$*" >&2; }
die() {
  err "$@"
  exit 1
}

detect_compose() {
  if command -v docker >/dev/null 2>&1; then
    echo "docker compose"
  elif command -v podman-compose >/dev/null 2>&1; then
    echo "podman-compose"
  elif command -v podman >/dev/null 2>&1; then
    echo "podman compose"
  else
    die "No container runtime found. Install docker or podman."
  fi
}

COMPOSE_CMD=$(detect_compose)

wait_for_pg() {
  local dsn=$1 label=$2 timeout=${3:-60}
  local deadline=$((SECONDS + timeout))
  while ! psql "$dsn" -c "SELECT 1" >/dev/null 2>&1; do
    if ((SECONDS >= deadline)); then
      die "$label not ready after ${timeout}s"
    fi
    sleep 1
  done
  ok "$label is ready"
}

fmt_count() {
  local n=$1
  if ((n >= 1000000000)); then
    awk "BEGIN { printf \"%.1fB\", $n/1000000000 }"
  elif ((n >= 1000000)); then
    awk "BEGIN { printf \"%.1fM\", $n/1000000 }"
  elif ((n >= 1000)); then
    awk "BEGIN { printf \"%.1fK\", $n/1000 }"
  else
    echo "$n"
  fi
}

fmt_bytes() {
  local b=$1
  if ((b >= 1073741824)); then
    awk "BEGIN { printf \"%.2f GB\", $b/1073741824 }"
  elif ((b >= 1048576)); then
    awk "BEGIN { printf \"%.1f MB\", $b/1048576 }"
  else
    awk "BEGIN { printf \"%.1f KB\", $b/1024 }"
  fi
}

elapsed_since() {
  local start=$1
  local secs=$((SECONDS - start))
  printf "%dm%02ds" $((secs / 60)) $((secs % 60))
}

# ── Parse args ───────────────────────────────────────────────────────────────

while [[ $# -gt 0 ]]; do
  case $1 in
  --size)
    TARGET_GB=$2
    shift 2
    ;;
  --down)
    log "Tearing down bench containers..."
    $COMPOSE_CMD -f "$COMPOSE_FILE" down -v
    ok "Done"
    exit 0
    ;;
  -h | --help)
    echo "Usage: $0 [--size GB] [--down]"
    echo ""
    echo "Options:"
    echo "  --size GB   Target dataset size (default: 50)"
    echo "  --down      Tear down containers and exit"
    exit 0
    ;;
  *) die "Unknown argument: $1" ;;
  esac
done

TOTAL_ROWS=$((TARGET_GB * 1073741824 / APPROX_ROW_BYTES))

# ── Table definitions (name, percentage of total rows) ───────────────────────

TABLES=(
  "bench_events:40"
  "bench_logs:25"
  "bench_metrics:20"
  "bench_users:10"
  "bench_sessions:5"
)

# ── Signal handling ──────────────────────────────────────────────────────────

cleanup() {
  err "Interrupted — killing all background workers..."
  kill 0 2>/dev/null
  wait 2>/dev/null
  exit 130
}
trap cleanup INT TERM

# ── Start containers ─────────────────────────────────────────────────────────

log "Starting bench containers (${TARGET_GB}GB target)..."
$COMPOSE_CMD -f "$COMPOSE_FILE" up -d 2>&1 | tail -2

log "Waiting for databases..."
wait_for_pg "$SOURCE_DSN" "source-pg" 60
wait_for_pg "postgres://postgres:dest@localhost:55433/dest" "dest-pg" 60

log "Enabling pg_stat_statements..."
psql "$SOURCE_DSN" -q -c "CREATE EXTENSION IF NOT EXISTS pg_stat_statements;" >/dev/null 2>&1
psql "postgres://postgres:dest@localhost:55433/dest" -q -c "CREATE EXTENSION IF NOT EXISTS pg_stat_statements;" >/dev/null 2>&1
ok "pg_stat_statements enabled on both containers"

# ── Seed function ────────────────────────────────────────────────────────────

seed_table() {
  local name=$1 rows=$2

  psql "$SOURCE_DSN" -q <<SQL >/dev/null 2>&1
CREATE TABLE IF NOT EXISTS _seed_progress (
    table_name TEXT PRIMARY KEY,
    rows_done  BIGINT NOT NULL DEFAULT 0,
    phase      TEXT NOT NULL DEFAULT 'inserting'
);
INSERT INTO _seed_progress (table_name, rows_done)
    VALUES ('$name', 0)
    ON CONFLICT (table_name) DO UPDATE SET rows_done = 0;
DROP TABLE IF EXISTS "$name" CASCADE;
CREATE UNLOGGED TABLE "$name" (
    id         BIGINT      NOT NULL,
    ts         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    category   SMALLINT    NOT NULL,
    source_id  TEXT        NOT NULL,
    payload    TEXT        NOT NULL,
    score      NUMERIC(12,4) NOT NULL DEFAULT 0,
    tags       TEXT[]      NOT NULL DEFAULT '{}',
    metadata   JSONB       NOT NULL DEFAULT '{}'
);
SQL

  local remaining=$rows
  local pids=()
  local id_offset=0

  for ((w = 0; w < WORKERS_PER_TABLE; w++)); do
    local worker_rows=$((remaining / (WORKERS_PER_TABLE - w)))
    remaining=$((remaining - worker_rows))
    local worker_offset=$id_offset
    id_offset=$((id_offset + worker_rows))
    (
      psql "$SOURCE_DSN" -q <<WORKER_SQL >/dev/null 2>&1
SET synchronous_commit = off;
CREATE OR REPLACE PROCEDURE _seed_${name}_w${w}() LANGUAGE plpgsql AS \$proc\$
DECLARE
    batch_start BIGINT := ${worker_offset};
    batch_end   BIGINT := ${worker_offset} + ${worker_rows};
    chunk       INT    := ${BATCH_SIZE};
    i           BIGINT := batch_start;
BEGIN
    WHILE i < batch_end LOOP
        INSERT INTO "$name" (id, ts, category, source_id, payload, score, tags, metadata)
        SELECT
            i + g,
            NOW() - (random() * INTERVAL '365 days'),
            (random() * 100)::SMALLINT,
            md5(random()::TEXT),
            REPEAT('x', 300),
            (random() * 100000)::NUMERIC(12,4),
            ARRAY['tag-' || (random()*50)::INT, 'cat-' || (random()*20)::INT],
            '{"k":1}'::JSONB
        FROM generate_series(0, LEAST(chunk, batch_end - i) - 1) AS g;
        UPDATE _seed_progress SET rows_done = rows_done + LEAST(chunk, batch_end - i) WHERE table_name = '$name';
        i := i + chunk;
        COMMIT;
    END LOOP;
END
\$proc\$;
CALL _seed_${name}_w${w}();
DROP PROCEDURE _seed_${name}_w${w}();
WORKER_SQL
    ) &
    pids+=($!)
  done

  local failed=0
  for pid in "${pids[@]}"; do
    if ! wait "$pid"; then
      failed=1
    fi
  done

  if ((failed)); then
    exit 1
  fi

  psql "$SOURCE_DSN" -q -c "UPDATE _seed_progress SET phase = 'adding PK' WHERE table_name = '$name';" >/dev/null 2>&1
  psql "$SOURCE_DSN" -q -c "ALTER TABLE \"$name\" ADD PRIMARY KEY (id);" >/dev/null 2>&1

  psql "$SOURCE_DSN" -q -c "UPDATE _seed_progress SET phase = 'set logged' WHERE table_name = '$name';" >/dev/null 2>&1
  psql "$SOURCE_DSN" -q -c "ALTER TABLE \"$name\" SET LOGGED;" >/dev/null 2>&1

  psql "$SOURCE_DSN" -q -c "UPDATE _seed_progress SET phase = 'analyzing' WHERE table_name = '$name';" >/dev/null 2>&1
  psql "$SOURCE_DSN" -q -c "ANALYZE \"$name\";" >/dev/null 2>&1

  psql "$SOURCE_DSN" -q -c "UPDATE _seed_progress SET phase = 'done' WHERE table_name = '$name';" >/dev/null 2>&1
}

# ── Progress display ─────────────────────────────────────────────────────────

TOTAL_WORKERS=$((WORKERS_PER_TABLE * ${#TABLES[@]}))
log "Using $WORKERS_PER_TABLE workers/table ($TOTAL_WORKERS total, $TOTAL_CPUS CPUs detected)"
NUM_TABLES=${#TABLES[@]}
MONITOR_LINES=$((NUM_TABLES + 2))

print_progress() {
  local table_names=()
  local table_targets=()
  for entry in "${TABLES[@]}"; do
    table_names+=("${entry%%:*}")
    local pct=${entry##*:}
    table_targets+=($((TOTAL_ROWS * pct / 100)))
  done

  local grand_inserted=0
  local elapsed=$((SECONDS - SEED_START))
  local elapsed_str
  elapsed_str=$(printf "%dm%02ds" $((elapsed / 60)) $((elapsed % 60)))

  printf "  \033[1mSeeding %d tables  [%s elapsed]\033[0m\033[K\n" "$NUM_TABLES" "$elapsed_str"

  for ((i = 0; i < NUM_TABLES; i++)); do
    local name=${table_names[$i]}
    local target=${table_targets[$i]}
    local inserted
    local progress_row
    progress_row=$(psql "$SOURCE_DSN" -t -A -c \
      "SELECT COALESCE(rows_done, 0) || ':' || COALESCE(phase, 'inserting') FROM _seed_progress WHERE table_name = '$name';" 2>/dev/null || echo "0:inserting")
    progress_row=${progress_row:-0:inserting}
    inserted=${progress_row%%:*}
    local phase=${progress_row#*:}
    inserted=${inserted:-0}
    grand_inserted=$((grand_inserted + inserted))
    local pct=0
    if ((target > 0)); then
      pct=$((inserted * 100 / target))
      if ((pct > 100)); then pct=100; fi
    fi
    local filled=$((pct * 30 / 100))
    local empty=$((30 - filled))
    local bar=""
    for ((b = 0; b < filled; b++)); do bar+="█"; done
    for ((b = 0; b < empty; b++)); do bar+="░"; done
    local status_str
    if [[ "$phase" == "inserting" ]]; then
      status_str=$(printf "%s / %s" "$(fmt_count $inserted)" "$(fmt_count $target)")
    elif [[ "$phase" == "done" ]]; then
      status_str="done"
    else
      status_str="$phase..."
    fi
    printf "  %-18s %s %3d%%  %s\033[K\n" \
      "$name" "$bar" "$pct" "$status_str"
  done

  local grand_pct=0
  if ((TOTAL_ROWS > 0)); then
    grand_pct=$((grand_inserted * 100 / TOTAL_ROWS))
    if ((grand_pct > 100)); then grand_pct=100; fi
  fi
  local rate=0
  if ((elapsed > 0)); then
    rate=$((grand_inserted / elapsed))
  fi
  printf "  \033[1mTotal: %3d%%  %s / %s  (%s rows/s)\033[0m\033[K\n" \
    "$grand_pct" "$(fmt_count $grand_inserted)" "$(fmt_count $TOTAL_ROWS)" "$(fmt_count $rate)"
}

any_alive() {
  for pid in "${seed_pids[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
  done
  return 1
}

# ── Seed all tables in parallel ──────────────────────────────────────────────

log "Seeding ${#TABLES[@]} tables — target ~${TARGET_GB}GB ($(fmt_count $TOTAL_ROWS) rows total)"

SEED_START=$SECONDS
seed_pids=()

for entry in "${TABLES[@]}"; do
  name=${entry%%:*}
  pct=${entry##*:}
  rows=$((TOTAL_ROWS * pct / 100))
  seed_table "$name" "$rows" &
  seed_pids+=($!)
done

# Reserve lines for progress display
for ((i = 0; i < MONITOR_LINES; i++)); do echo ""; done

# Poll progress until all seed workers exit
while any_alive; do
  printf "\033[${MONITOR_LINES}A"
  print_progress
  sleep 1
done

# Final draw at 100%
printf "\033[${MONITOR_LINES}A"
print_progress

# Collect exit codes
failed=0
for pid in "${seed_pids[@]}"; do
  if ! wait "$pid" 2>/dev/null; then
    failed=1
  fi
done

if ((failed)); then
  die "One or more tables failed to seed"
fi

psql "$SOURCE_DSN" -q -c "DROP TABLE IF EXISTS _seed_progress;" >/dev/null 2>&1

log "Seeding complete in $(elapsed_since $SEED_START)"
echo ""

# ── Summary ──────────────────────────────────────────────────────────────────

log "Source data summary:"
echo ""
printf "  %-22s %12s %12s\n" "TABLE" "ROWS" "SIZE"
printf "  %-22s %12s %12s\n" "─────" "────" "────"

total_rows=0
total_size=0

for entry in "${TABLES[@]}"; do
  name=${entry%%:*}
  est=$(psql "$SOURCE_DSN" -t -A -c "
        SELECT COALESCE(c.reltuples,0)::BIGINT || ':' || COALESCE(pg_table_size(c.oid),0)
        FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relname = '$name';
    ")
  r=${est%%:*}
  s=${est##*:}
  total_rows=$((total_rows + r))
  total_size=$((total_size + s))
  printf "  %-22s %12s %12s\n" "$name" "$(fmt_count $r)" "$(fmt_bytes $s)"
done

printf "  %-22s %12s %12s\n" "─────" "────" "────"
printf "  %-22s %12s %12s\n" "TOTAL" "$(fmt_count $total_rows)" "$(fmt_bytes $total_size)"
echo ""

log "Containers ready. Source: localhost:55432, Dest: localhost:55433"
echo ""
echo "  Run migration:  ./pgmanager clone --source-uri '$SOURCE_DSN' --dest-uri 'postgres://postgres:dest@localhost:55433/dest'"
echo "  Tear down:      $0 --down"
echo ""
