#!/usr/bin/env bash
set -euo pipefail

plugin_id=io.github.pabloduke.jot
source_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
target_dir="${XDG_CONFIG_HOME:-$HOME/.config}/omarchy/plugins/$plugin_id"

omarchy plugin validate "$source_dir"
mkdir -p "$target_dir"
# Copy the entry points first and the manifest last so a live shell never sees
# a manifest that points at files which have not arrived yet.
cp "$source_dir/BarWidget.qml" "$source_dir/Panel.qml" "$target_dir/"
cp "$source_dir/manifest.json" "$target_dir/"
omarchy-shell shell rescanPlugins
omarchy plugin enable "$plugin_id" --section right --before omarchy.power
