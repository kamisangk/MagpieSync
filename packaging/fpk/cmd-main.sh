#!/bin/bash
set -eu

pid_file="${TRIM_PKGVAR}/magpiesync.pid"
log_file="${TRIM_PKGVAR}/magpiesync.log"
data_dir="${TRIM_PKGVAR}/data"
app_bin="${TRIM_APPDEST}/bin/magpiesync"
socket_path="${TRIM_APPDEST}/magpiesync.sock"
temp_log=${TRIM_TEMP_LOGFILE:-/dev/stderr}

status_app() {
  if [ -f "$pid_file" ]; then
    pid=$(head -n 1 "$pid_file" | tr -d '[:space:]')
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then return 0; fi
    rm -f "$pid_file"
  fi
  return 1
}

case "${1:-}" in
  start)
    status_app && exit 0
    mkdir -p "$data_dir"
    if [ -L "$socket_path" ] || { [ -e "$socket_path" ] && [ ! -S "$socket_path" ]; }; then
      echo "Refusing to replace non-socket path: $socket_path" > "$temp_log"
      exit 1
    fi
    if [ -S "$socket_path" ]; then rm -f "$socket_path"; fi
    export PATH="${TRIM_APPDEST}/bin/ffmpeg:${TRIM_APPDEST}/bin:/usr/local/bin:/usr/bin:/bin"
    cd "$TRIM_PKGVAR"
    nohup "$app_bin" -unix-socket "$socket_path" -base-path /app/magpiesync -data-dir "$data_dir" >>"$log_file" 2>&1 &
    pid=$!
    echo "$pid" > "$pid_file"
    sleep 1
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$pid_file"
      if [ -n "${TRIM_TEMP_LOGFILE:-}" ]; then
        tail -n 30 "$log_file" > "$TRIM_TEMP_LOGFILE"
      fi
      exit 1
    fi
    ;;
  stop)
    if status_app; then
      pid=$(head -n 1 "$pid_file" | tr -d '[:space:]')
      kill -TERM "$pid" 2>>"$log_file" || true
      i=0
      while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 10 ]; do sleep 1; i=$((i + 1)); done
      kill -KILL "$pid" 2>/dev/null || true
    fi
    rm -f "$pid_file"
    if [ -S "$socket_path" ]; then rm -f "$socket_path"; fi
    ;;
  status)
    status_app && exit 0
    exit 3
    ;;
  *)
    echo "usage: $0 start|stop|status" > "${TRIM_TEMP_LOGFILE:-/dev/stderr}"
    exit 1
    ;;
esac
