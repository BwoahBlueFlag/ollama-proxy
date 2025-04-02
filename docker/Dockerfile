FROM ollama/ollama:0.6.1
RUN apt-get update && apt-get install -y socat
COPY model /models/model
COPY run.sh /scripts/run.sh
RUN chmod +x /scripts/run.sh
ENTRYPOINT ["/scripts/run.sh"]
