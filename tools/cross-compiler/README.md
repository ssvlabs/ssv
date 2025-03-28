# SSV Node Cross-Compilation Toolchain

This directory contains tools and scripts to facilitate cross-compilation of the SSV node for different architectures.

## Overview

The cross-compilation toolchain enables building the SSV node for multiple architectures (currently amd64 and arm64)
from a single build environment. This is achieved by:

1. Using compiler wrappers to handle architecture-specific flags
2. Setting up the proper environment variables and paths
3. Using Docker BuildKit for multi-architecture builds

## Directory Structure

- `scripts/` - Contains shell scripts for the build process
    - `install_deps.sh` - Installs necessary dependencies for cross-compilation
    - `create_wrappers.sh` - Creates compiler wrapper scripts to handle cross-compilation flags
    - `build.sh` - Builds the SSV node for a specific target architecture

## Usage

### Building Multi-Architecture Docker Images

To build multi-architecture Docker images, run:

```bash
IMAGE_NAME=your-registry/your-image:tag SOURCE_COMMIT=$(git rev-parse HEAD) ./hooks/build
```

This will:

1. Create a Docker BuildKit builder
2. Build the SSV node for both amd64 and arm64 architectures
3. Push the resulting multi-architecture image

Required environment variables:

- `IMAGE_NAME`: The full name and tag for the Docker image to be built and pushed
- `SOURCE_COMMIT`: Git commit to be used as the image label

### Manual Cross-Compilation

To manually cross-compile for a specific architecture:

1. Build the Docker image for the build environment:
   ```bash
   docker build --target builder -t ssv-builder .
   ```

2. Run a container with the build environment:
   ```bash
   docker run -it --rm -v $(pwd):/go/src/github.com/ssvlabs/ssv ssv-builder bash
   ```

3. Inside the container, build for a specific architecture:
   ```bash
   /tmp/scripts/build.sh amd64 /go/bin/ssvnode-amd64
   # or
   /tmp/scripts/build.sh arm64 /go/bin/ssvnode-arm64
   ```

## Notes on Linking

The build process uses dynamic linking to ensure maximum compatibility across different environments. This approach was
chosen after encountering various issues with static linking in cross-compilation scenarios.

## Troubleshooting

If you encounter issues with cross-compilation:

1. Check that all required dependencies are installed
2. Verify that the compiler wrappers are functioning correctly
3. Ensure that the target architecture is supported

For more detailed logs, you can run the Docker build with:

```bash
BUILDKIT_PROGRESS=plain docker buildx build --progress=plain ...
``` 
