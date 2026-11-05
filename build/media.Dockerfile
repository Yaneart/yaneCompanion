FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587 AS builder

# A dated apt snapshot fixes transitive compiler/build dependencies.
RUN rm /etc/apt/sources.list.d/debian.sources \
 && printf 'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/20261105T000000Z/ bookworm main\n' > /etc/apt/sources.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends build-essential gcc-mingw-w64-x86-64-posix binutils-mingw-w64-x86-64 gnupg xz-utils ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY artifacts/ /sources/
COPY build/media.sh /build/media.sh
ARG REBUILD=1
RUN sh /build/media.sh
FROM scratch
COPY --from=builder /output/ /
