#!/bin/sh
# Local packaging only. Caller must obtain current build admission/lease first.
# This script never uploads an image or creates cloud resources.
set -eu
: "${OPS_BIN:?Set OPS_BIN to the qualified OPS 0.1.47 executable}"
: "${ARTIFACT_DIR:?Set ARTIFACT_DIR to a task-owned BuildOffload directory}"
elf=${1:?Usage: package-nanos.sh /absolute/path/to/linux-amd64-ELF}
case "$elf" in /*) ;; *) echo 'ELF path must be absolute' >&2; exit 2;; esac
case "$ARTIFACT_DIR" in /Volumes/BuildOffload/*) ;; *) echo 'Use a task-owned BuildOffload artifact directory' >&2; exit 2;; esac
test -z "$(git status --porcelain)" || { echo 'Commit or remove source-tree changes before packaging' >&2; exit 2; }
source_commit=$(git rev-parse HEAD)
go_toolchain=$(go version)
printf '%s' "$go_toolchain" | grep -F 'go1.26.0' >/dev/null || { echo 'Expected pinned Go 1.26.0 toolchain' >&2; exit 2; }
elf_sha256=$(shasum -a 256 "$elf" | awk '{print $1}')
"$OPS_BIN" version | head -1 | grep -Fx 'Ops version: 0.1.47' >/dev/null
python3 - "$elf" <<'PY'
import sys
with open(sys.argv[1],'rb') as f:h=f.read(20)
if h[:4]!=b'\x7fELF' or h[4:6]!=b'\x02\x01' or int.from_bytes(h[18:20],'little')!=62:
 raise SystemExit('Expected a 64-bit little-endian x86 ELF')
PY
mkdir -p "$ARTIFACT_DIR"
# # OPS_HOME points to the parent of .ops; preserve the real HOME unchanged.
export OPS_HOME="$ARTIFACT_DIR/ops-cache"
: "${CA_DIR:?Set CA_DIR to a directory containing only ca-certificates.crt}"
python3 scripts/nanos-config.py --mode gcp --ca-dir "$CA_DIR" --image "$ARTIFACT_DIR/disk.raw" > "$ARTIFACT_DIR/nanos.json"
"$OPS_BIN" build "$elf" --arch amd64 --nanos-version 0.1.56 --target-cloud onprem --imagename is-optimized --config "$ARTIFACT_DIR/nanos.json" > "$ARTIFACT_DIR/package.log" 2>&1
image="$OPS_HOME/.ops/images/is-optimized"
test -s "$image"
image_sha256=$(shasum -a 256 "$image" | awk '{print $1}')
image_bytes=$(wc -c < "$image" | tr -d ' ')
cp "$image" "$ARTIFACT_DIR/disk.raw"
# Compute Engine's manual raw-disk import requires disk.raw in 1 GiB increments.
# Extend sparsely so only the compressed archive, not a dense 1 GiB file, is stored locally.
python3 - "$ARTIFACT_DIR/disk.raw" <<'PY'
from pathlib import Path
import sys
p=Path(sys.argv[1])
unit=1<<30
size=p.stat().st_size
p.touch(exist_ok=True)
with p.open('r+b') as f:
    f.truncate(((size+unit-1)//unit)*unit)
PY
python3 - "$ARTIFACT_DIR" "$source_commit" "$go_toolchain" "$elf_sha256" "$image_sha256" "$image_bytes" <<'PY'
from pathlib import Path
import hashlib,json,sys,tarfile
p=Path(sys.argv[1]);raw=p/'disk.raw'
with tarfile.open(p/'image.tar.gz','w:gz',format=tarfile.GNU_FORMAT) as tar:tar.add(raw,arcname='disk.raw')
archive=p/'image.tar.gz'
receipt={'source_commit':sys.argv[2],'go_toolchain':sys.argv[3],'elf_sha256':sys.argv[4],'ops':'0.1.47','nanos':'0.1.56','architecture':'amd64','image_filename':'is-optimized','image_bytes_before_gcp_padding':int(sys.argv[6]),'image_sha256_before_gcp_padding':sys.argv[5],'raw_filename':'disk.raw','raw_bytes_after_gcp_padding':raw.stat().st_size,'raw_sha256':hashlib.file_digest(raw.open('rb'),'sha256').hexdigest(),'archive_filename':archive.name,'archive_bytes':archive.stat().st_size,'archive_sha256':hashlib.file_digest(archive.open('rb'),'sha256').hexdigest(),'qualification':'packaged only; Nanos boot and GCP compatibility unverified'}
(p/'image-receipt.json').write_text(json.dumps(receipt,indent=2)+'\n')
print(json.dumps(receipt))
PY
