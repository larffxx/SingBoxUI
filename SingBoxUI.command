#!/bin/sh
# SingBoxUI — запуск двойным кликом на macOS (откроется Terminal).
cd "$(dirname "$0")" || exit 1
exec ./run.sh
