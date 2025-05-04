#!/bin/sh

# ollama runner is hardcoded to listen on 127.0.0.1
socat TCP-LISTEN:57156,fork TCP:127.0.0.1:57157 &
exec /bin/ollama "$@"
