FROM python:3.12-slim@sha256:3d5ed973e45820f5ba5e46bd065bd88b3a504ff0724d85980dcd05eab361fcf4

WORKDIR /app

# Install gcloud CLI (needed for local token fallback only — on Cloud Run uses metadata server)
RUN apt-get update && apt-get install -y curl gnupg && \
    echo "deb [signed-by=/usr/share/keyrings/cloud.google.asc] https://packages.cloud.google.com/apt cloud-sdk main" \
        > /etc/apt/sources.list.d/google-cloud-sdk.list && \
    curl -s https://packages.cloud.google.com/apt/doc/apt-key.gpg \
        -o /usr/share/keyrings/cloud.google.asc && \
    apt-get update && apt-get install -y google-cloud-cli && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

COPY enrichment_watchdog.py .

CMD ["python", "enrichment_watchdog.py"]
