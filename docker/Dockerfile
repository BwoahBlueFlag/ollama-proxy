FROM golang AS builder
WORKDIR /ollama-proxy
ENV CGO_ENABLED=0
RUN git clone https://github.com/BwoahBlueFlag/ollama-proxy.git .
RUN go build -o ollama-proxy main.go deleteRunner.go
RUN go build -o ollama-proxy-watchdog watchdog.go deleteRunner.go

FROM ollama/ollama:0.6.1

RUN apt-get update && apt-get install -y socat

WORKDIR /ollama-proxy

COPY --from=builder /ollama-proxy/ollama-proxy .
COPY --from=builder /ollama-proxy/ollama-proxy-watchdog .

COPY run-runner.sh .
RUN chmod +x run-runner.sh

COPY run-and-replace.sh .
RUN chmod +x run-and-replace.sh

RUN cp "$(which ollama)" ./ollama-real

ENV OLLAMA_HOST=0.0.0.0:11434
ENV OLLAMA_MODELS=/mnt/models

ENTRYPOINT ["./run-and-replace.sh"]
