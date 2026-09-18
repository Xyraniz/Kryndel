#!/bin/sh
set -eu

# Installs a per-user association; no root permissions are required.
# Usage: tools/install-association.sh [/absolute/path/to/kry]
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
kry=${1:-"$root/build/kry"}
if [ ! -x "$kry" ]; then
    printf '%s\n' "kry: executable not found or not executable: $kry" >&2
    exit 69
fi

applications=${XDG_DATA_HOME:-"$HOME/.local/share"}/applications
mime_dir=${XDG_DATA_HOME:-"$HOME/.local/share"}/mime/packages
mkdir -p "$applications" "$mime_dir"

cat > "$applications/kryndel.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Kryndel
Comment=Run Kryndel source and native artifacts
Exec=$kry %f
Terminal=true
MimeType=application/x-kryndel-source;application/x-kryndel-artifact;
NoDisplay=true
EOF

cat > "$mime_dir/kryndel.xml" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<mime-info>
  <mime-type type="application/x-kryndel-source"><comment>Kryndel source</comment><glob pattern="*.kry"/></mime-type>
  <mime-type type="application/x-kryndel-artifact"><comment>Kryndel artifact</comment><glob pattern="*.kexe"/></mime-type>
</mime-info>
EOF
update-mime-database "${XDG_DATA_HOME:-"$HOME/.local/share"}/mime" 2>/dev/null || true
xdg-mime default kryndel.desktop application/x-kryndel-source 2>/dev/null || true
xdg-mime default kryndel.desktop application/x-kryndel-artifact 2>/dev/null || true
printf '%s\n' "Kryndel association installed for .kry and .kexe"
