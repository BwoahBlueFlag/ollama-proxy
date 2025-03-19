#!/bin/sh

# requires ollama binary named "ollama-real" and proxy binary named "ollama-proxy"

cp ollama-real ollama
./ollama serve > /dev/null 2>&1 &

# the binary has to be deleted first otherwise the original process is killed
rm ollama
cp ollama-proxy ollama
