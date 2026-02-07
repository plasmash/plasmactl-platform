# Multi-platform Rust cross-compilation for platform-graph.
# Produces binaries for linux/darwin/windows × amd64/arm64.
#
# Usage (via Makefile):
#   make graph-all

FROM rust:1-bookworm AS builder

# Install zig (universal cross-linker) and cargo-zigbuild.
ARG ZIG_VERSION=0.13.0
RUN curl -sSfL "https://ziglang.org/download/${ZIG_VERSION}/zig-linux-x86_64-${ZIG_VERSION}.tar.xz" \
        | tar xJ -C /opt \
    && ln -s "/opt/zig-linux-x86_64-${ZIG_VERSION}/zig" /usr/local/bin/zig \
    && cargo install cargo-zigbuild

# Add all Rust targets.
RUN rustup target add \
    x86_64-unknown-linux-musl \
    aarch64-unknown-linux-musl \
    x86_64-apple-darwin \
    aarch64-apple-darwin \
    x86_64-pc-windows-gnu

WORKDIR /build

# Cache dependency layer: copy manifests, build a dummy crate, then remove it.
# Remove the binary + fingerprint so cargo recompiles platform-graph with real source.
COPY Cargo.toml Cargo.lock ./
RUN mkdir src && echo "fn main() {}" > src/main.rs \
    && cargo zigbuild --release --target x86_64-unknown-linux-musl \
    && rm -rf src \
       target/x86_64-unknown-linux-musl/release/platform-graph \
       target/x86_64-unknown-linux-musl/release/.fingerprint/platform-graph-*

# Copy real source and build all targets.
COPY src/ src/

RUN cargo zigbuild --release --target x86_64-unknown-linux-musl \
    && cargo zigbuild --release --target aarch64-unknown-linux-musl \
    && cargo zigbuild --release --target x86_64-apple-darwin \
    && cargo zigbuild --release --target aarch64-apple-darwin \
    && cargo zigbuild --release --target x86_64-pc-windows-gnu

# Collect binaries with Go-convention names.
FROM busybox:musl AS output
COPY --from=builder /build/target/x86_64-unknown-linux-musl/release/platform-graph    /platform-graph-linux-amd64
COPY --from=builder /build/target/aarch64-unknown-linux-musl/release/platform-graph   /platform-graph-linux-arm64
COPY --from=builder /build/target/x86_64-apple-darwin/release/platform-graph          /platform-graph-darwin-amd64
COPY --from=builder /build/target/aarch64-apple-darwin/release/platform-graph         /platform-graph-darwin-arm64
COPY --from=builder /build/target/x86_64-pc-windows-gnu/release/platform-graph.exe   /platform-graph-windows-amd64.exe
