# Use a maintained Go + Alpine tag compatible with the module toolchain
FROM golang:1.25-alpine3.22

# Set environment variables
ENV PATH="$PATH:/bin/bash" \
  BENTO4_BIN="/opt/bento4/bin" \
  PATH="$PATH:/opt/bento4/bin"

# Install necessary packages including FFMPEG, Bash, and build tools
RUN apk add --no-cache \
  ffmpeg \
  bash \
  make \
  python3 \
  unzip \
  gcc \
  g++ \
  scons \
  git

# Install Bento4
WORKDIR /tmp/bento4
ENV BENTO4_BASE_URL="https://www.bok.net/Bento4/binaries" \
  BENTO4_SDK="Bento4-SDK-1-6-0-641.x86_64-unknown-linux.zip" \
  BENTO4_PATH="/opt/bento4"

# Download and install Bento4 prebuilt SDK (avoids legacy Python2 build scripts)
RUN wget -q -O Bento4.zip ${BENTO4_BASE_URL}/${BENTO4_SDK} && \
  unzip Bento4.zip -d /opt && \
  rm Bento4.zip && \
  mv /opt/Bento4-SDK-* ${BENTO4_PATH} && \
  chmod +x ${BENTO4_PATH}/bin/*

# Set the working directory to the Go source directory
WORKDIR /go/src

# Change the entry point to start the application
ENTRYPOINT ["tail", "-f", "/dev/null"]
