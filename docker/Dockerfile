FROM golang AS builder
WORKDIR /ollama-proxy
RUN git clone https://github.com/BwoahBlueFlag/ollama-proxy.git .
RUN go build -o ollama-proxy main.go
RUN go build -o ollama-proxy-watchdog watchdog.go

FROM ollama/ollama:0.6.1

COPY model /models/model

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

ENTRYPOINT ["./run-and-replace.sh"]
