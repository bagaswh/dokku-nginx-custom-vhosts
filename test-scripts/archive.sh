#!/usr/bin/env bash
set -eo pipefail

script_dir="$(dirname "$0")"
tar -czf "$script_dir/archive.tar.gz" "$script_dir/../"