#!/bin/sh
set -eu
ami_install_dir=${AMI_INSTALL_DIR:-"$HOME/.local/bin"}
ami_release_base=${AMI_RELEASE_BASE_URL:-https://github.com/dogxii/ami/releases/download}
ami_version=${AMI_VERSION:-latest}
case "$(uname -s)" in Darwin) ami_os=darwin ;; Linux) ami_os=linux ;; *) echo 'Use the Windows executable from GitHub Releases.' >&2; exit 1 ;; esac
case "$(uname -m)" in arm64|aarch64) ami_arch=arm64 ;; x86_64|amd64) ami_arch=amd64 ;; *) echo 'Unsupported CPU architecture.' >&2; exit 1 ;; esac
if [ "$ami_version" = latest ]; then
  ami_api=${AMI_GITHUB_API_URL:-https://api.github.com/repos/dogxii/ami/releases/latest}
  ami_version=$(curl -fsSL "$ami_api" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
fi
ami_version=${ami_version#v}
case "$ami_version" in ''|*[!0-9.]*|.*|*..*|*.) echo 'Invalid release version.' >&2; exit 1 ;; esac
ami_asset="ami_${ami_os}_${ami_arch}"
mkdir -p "$ami_install_dir"
ami_tmp=$(mktemp -d "$ami_install_dir/.ami-install.XXXXXX")
trap 'rm -rf "$ami_tmp"' EXIT HUP INT TERM
curl -fsSL "$ami_release_base/v$ami_version/checksums.txt" -o "$ami_tmp/checksums.txt"
curl -fsSL "$ami_release_base/v$ami_version/$ami_asset" -o "$ami_tmp/$ami_asset"
ami_expected=$(awk -v name="$ami_asset" '{file=$2;sub(/^\*/,"",file);if(file==name)print $1}' "$ami_tmp/checksums.txt")
if command -v sha256sum >/dev/null 2>&1; then
  ami_actual=$(sha256sum "$ami_tmp/$ami_asset" | awk '{print $1}')
else
  ami_actual=$(shasum -a 256 "$ami_tmp/$ami_asset" | awk '{print $1}')
fi
if [ -z "$ami_expected" ] || [ "$ami_actual" != "$ami_expected" ]; then echo 'Executable checksum mismatch.' >&2; exit 1; fi
chmod 755 "$ami_tmp/$ami_asset"
mv -f "$ami_tmp/$ami_asset" "$ami_install_dir/ami"
printf 'Installed ami %s to %s/ami\n' "$ami_version" "$ami_install_dir"
printf 'Add %s to PATH if needed.\n' "$ami_install_dir"
