FROM python:3.12-slim-bookworm

RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY pyproject.toml ./
COPY footage_worker ./footage_worker
RUN pip install --no-cache-dir .

# New named volumes copy these directories' owner, so the worker can write.
RUN useradd --uid 1000 --create-home footage && mkdir -p /data /models && chown footage /data /models
# Models download here on first use; mount a volume to keep them.
ENV HF_HOME=/models PYTHONUNBUFFERED=1
USER footage
CMD ["footage-worker"]
