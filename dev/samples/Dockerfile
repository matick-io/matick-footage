# Generates synthetic recordings with sidecars and spoken lines for local runs
# and tests. Nothing here is real footage.
FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg espeak-ng fonts-dejavu-core python3 \
 && rm -rf /var/lib/apt/lists/*
COPY make_samples.py /make_samples.py
ENTRYPOINT ["python3", "/make_samples.py", "/out"]
