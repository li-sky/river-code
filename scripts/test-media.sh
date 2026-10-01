#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../frontend"
task_media_dir="$(mktemp -d "$PWD/.media-test.XXXXXX")"
trap 'rm -rf "$task_media_dir"' EXIT
npx tsc src/lib/voice.ts --outDir "$task_media_dir" --target ES2022 --module ESNext --moduleResolution Bundler --esModuleInterop --skipLibCheck --strict
VOICE_MODULE="file://$task_media_dir/voice.js" node --test src/lib/voice.test.mjs
